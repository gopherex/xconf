// Package directory reads one string value per file, including projected secrets.
package directory

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
)

type Option func(*Source)
type Source struct {
	dir, id  string
	bindings map[string]x.Path
	interval time.Duration
	limit    int64
	trim     bool
}

func Name(id string) Option           { return func(s *Source) { s.id = id } }
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }

// MaxBytes limits the total bytes read in one directory snapshot.
func MaxBytes(n int64) Option { return func(s *Source) { s.limit = n } }

// TrimFinalNewline removes one LF or CRLF, preserving all other whitespace.
func TrimFinalNewline() Option { return func(s *Source) { s.trim = true } }

// New reads selected files into explicit object paths. Missing individual files
// contribute no value. Nil bindings select visible regular files by literal name
// at the root; empty non-nil bindings select none. Contents remain strings.
// Projected Kubernetes volumes are read from one pinned ..data generation.
func New(dir string, bindings map[string]x.Path, opts ...Option) *Source {
	s := &Source{dir: dir, id: "directory:" + dir, interval: 250 * time.Millisecond, limit: document.DefaultMaxBytes}
	if bindings != nil {
		s.bindings = map[string]x.Path{}
		for k, p := range bindings {
			s.bindings[k] = append(x.Path(nil), p...)
		}
	}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) generation() (string, error) {
	target, err := os.Readlink(filepath.Join(s.dir, "..data"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !filepath.IsLocal(target) {
		return "", errors.New("invalid projected volume generation")
	}
	return target, nil
}
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if s.limit <= 0 {
		return x.Layer{}, errors.New("invalid directory size limit")
	}
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return x.Layer{}, err
		}
		info, err := os.Stat(s.dir)
		if errors.Is(err, fs.ErrNotExist) {
			return x.Layer{}, x.ErrNotFound
		}
		if err != nil {
			return x.Layer{}, err
		}
		if !info.IsDir() {
			return x.Layer{}, errors.New("source is not a directory")
		}
		gen, err := s.generation()
		if err != nil {
			return x.Layer{}, err
		}
		base := s.dir
		if gen != "" {
			base = filepath.Join(s.dir, gen)
		}
		layer, readErr := s.read(ctx, base)
		after, err := s.generation()
		if err != nil {
			return x.Layer{}, err
		}
		if gen != after {
			continue
		}
		return layer, readErr
	}
	return x.Layer{}, errors.New("directory generation changed repeatedly")
}
func (s *Source) read(ctx context.Context, base string) (x.Layer, error) {
	bindings := s.bindings
	if bindings == nil {
		entries, err := os.ReadDir(base)
		if err != nil {
			return x.Layer{}, err
		}
		bindings = map[string]x.Path{}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") && !entry.IsDir() {
				bindings[entry.Name()] = x.Path{entry.Name()}
			}
		}
	}
	if len(bindings) > 1024 {
		return x.Layer{}, errors.New("directory file count exceeds limit")
	}
	names := make([]string, 0, len(bindings))
	for name, p := range bindings {
		if !fs.ValidPath(name) || name == "." || len(p) == 0 {
			return x.Layer{}, errors.New("invalid directory binding")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	// Reject ancestor/descendant bindings independently of which files exist.
	for i, a := range names {
		for _, b := range names[i+1:] {
			p, q := bindings[a], bindings[b]
			n := min(len(p), len(q))
			equal := true
			for j := 0; j < n; j++ {
				if p[j] != q[j] {
					equal = false
					break
				}
			}
			if equal {
				return x.Layer{}, errors.New("overlapping directory bindings")
			}
		}
	}
	values := map[string]any{}
	locations := map[string]x.Location{}
	hash := sha256.New()
	remaining := s.limit
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return x.Layer{}, err
		}
		filename := filepath.Join(base, name)
		// Reject devices and FIFOs before Open, which could otherwise block.
		info, err := os.Stat(filename)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return x.Layer{}, err
		}
		if !info.Mode().IsRegular() {
			return x.Layer{}, errors.New("secret file must be regular")
		}
		f, err := os.Open(filename)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return x.Layer{}, err
		}
		info, err = f.Stat()
		if err != nil {
			f.Close()
			return x.Layer{}, err
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return x.Layer{}, errors.New("secret file must be regular")
		}
		// Read accepts a positive bound, including for an empty file after the budget is exhausted.
		data, err := document.Read(ctx, f, max(remaining, 1))
		f.Close()
		if err != nil {
			return x.Layer{}, err
		}
		remaining -= int64(len(data))
		if remaining < 0 {
			return x.Layer{}, errors.New("directory exceeds size limit")
		}
		fmt.Fprintf(hash, "%d:%s%d:", len(name), name, len(data))
		hash.Write(data)
		value := string(data)
		if s.trim {
			value = strings.TrimSuffix(value, "\n")
			if strings.HasSuffix(string(data), "\r\n") {
				value = strings.TrimSuffix(value, "\r")
			}
		}
		path := bindings[name]
		obj := values
		for _, key := range path[:len(path)-1] {
			if obj[key] == nil {
				obj[key] = map[string]any{}
			}
			obj = obj[key].(map[string]any)
		}
		obj[path[len(path)-1]] = value
		locations[path.String()] = x.Location{Name: filepath.Join(s.dir, name)}
	}
	return x.Layer{Values: values, Locations: locations, Revision: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if s.interval == 0 {
		return func() {}, nil
	}
	return x.Poll(s, s.interval).(x.Watcher).Watch(ctx, notify)
}
