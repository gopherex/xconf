// Package file adapts a document decoder to a watched file source.
package file

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
)

// Decoder is the transport-independent document decoder.
type Decoder = x.Decoder
type Option func(*Source)
type Source struct {
	path, id string
	decode   Decoder
	interval time.Duration
	limit    int64
}

func Name(id string) Option           { return func(s *Source) { s.id = id } }
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }
func MaxBytes(n int64) Option         { return func(s *Source) { s.limit = n } }

// New polls complete file contents, detecting atomic replacement and recreation.
// Interval(0) disables watching. Optional files use xconf.Optional.
func New(path string, decode Decoder, opts ...Option) *Source {
	s := &Source{path: path, id: path, decode: decode, interval: 250 * time.Millisecond, limit: 8 << 20}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	f, err := os.Open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return x.Layer{}, x.ErrNotFound
	}
	if err != nil {
		return x.Layer{}, err
	}
	defer f.Close()
	data, err := document.Read(ctx, f, s.limit)
	if err != nil {
		return x.Layer{}, err
	}
	return document.Decode(data, s.decode, s.path, "")
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if s.interval == 0 {
		return func() {}, nil
	}
	return x.Poll(s, s.interval).(x.Watcher).Watch(ctx, notify)
}
