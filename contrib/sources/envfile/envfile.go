// Package envfile reads files named by explicitly bound *_FILE environment variables.
package envfile

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/env"
	"github.com/gopherex/xconf/internal/document"
	"os"
	"sort"
	"strings"
	"time"
)

type Option func(*Source)
type Source struct {
	id             string
	bindings       map[string]x.Path
	environment    []string
	injected, trim bool
	limit          int64
	interval       time.Duration
}

func Name(id string) Option { return func(s *Source) { s.id = id } }
func Environment(v []string) Option {
	return func(s *Source) { s.environment = append([]string(nil), v...); s.injected = true }
}
func MaxBytes(n int64) Option         { return func(s *Source) { s.limit = n } }
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }
func TrimFinalNewline() Option        { return func(s *Source) { s.trim = true } }

// New binds exact variable names ending in _FILE. Absent variables contribute
// nothing; a configured but missing/unreadable file fails, even under Optional.
// Setting both NAME and NAME_FILE is an error. Contents preserve whitespace.
func New(bindings map[string]x.Path, opts ...Option) *Source {
	s := &Source{id: "envfile", bindings: map[string]x.Path{}, limit: document.DefaultMaxBytes, interval: 250 * time.Millisecond}
	for n, p := range bindings {
		s.bindings[n] = append(x.Path(nil), p...)
	}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) Read(ctx context.Context, schema *sp.Schema) (x.Layer, error) {
	if s.limit <= 0 {
		return x.Layer{}, errors.New("invalid file size limit")
	}
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	entries := s.environment
	if !s.injected {
		entries = os.Environ()
	}
	vars := map[string]string{}
	for _, entry := range entries {
		if k, v, ok := strings.Cut(entry, "="); ok {
			vars[k] = v
		}
	}
	names := make([]string, 0, len(s.bindings))
	for n, p := range s.bindings {
		if !strings.HasSuffix(n, "_FILE") || n == "_FILE" || len(p) == 0 {
			return x.Layer{}, errors.New("invalid envfile binding")
		}
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 1024 {
		return x.Layer{}, errors.New("too many file bindings")
	}
	options := []env.Option{env.Prefix("__XCONF_ENVFILE_")}
	values := map[string]string{}
	locations := map[string]x.Location{}
	hash := sha256.New()
	remaining := s.limit
	for _, name := range names {
		path, ok := vars[name]
		if !ok {
			continue
		}
		if _, both := vars[strings.TrimSuffix(name, "_FILE")]; both {
			return x.Layer{}, fmt.Errorf("both direct and file environment values set for %s", name)
		}
		if path == "" {
			return x.Layer{}, fmt.Errorf("empty file path in %s", name)
		}
		info, err := os.Stat(path)
		if err != nil {
			return x.Layer{}, fmt.Errorf("file referenced by %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return x.Layer{}, errors.New("secret file must be regular")
		}
		f, err := os.Open(path)
		if err != nil {
			return x.Layer{}, err
		}
		data, readErr := document.Read(ctx, f, max(remaining, 1))
		closeErr := f.Close()
		if readErr != nil {
			return x.Layer{}, readErr
		}
		if closeErr != nil {
			return x.Layer{}, closeErr
		}
		remaining -= int64(len(data))
		if remaining < 0 {
			return x.Layer{}, errors.New("files exceed total size limit")
		}
		value := string(data)
		if s.trim {
			value = strings.TrimSuffix(value, "\n")
			if strings.HasSuffix(string(data), "\r\n") {
				value = strings.TrimSuffix(value, "\r")
			}
		}
		values[name] = value
		options = append(options, env.Bind(name, s.bindings[name]...))
		locations[s.bindings[name].String()] = x.Location{Name: name + ":" + path}
		fmt.Fprintf(hash, "%d:%s%d:%s%d:", len(name), name, len(path), path, len(data))
		hash.Write(data)
	}
	layer, err := env.Decode(schema, values, options...)
	if err != nil {
		return x.Layer{}, err
	}
	layer.Locations = locations
	layer.Revision = fmt.Sprintf("%x", hash.Sum(nil))
	return layer, nil
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if s.interval == 0 {
		return func() {}, nil
	}
	return x.Poll(s, s.interval).(x.Watcher).Watch(ctx, notify)
}
