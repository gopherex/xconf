// Package fs reads documents from fs.FS, including embed.FS and fstest.MapFS.
package fs

import (
	"context"
	"errors"
	"io/fs"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
)

type Option func(*Source)
type Source struct {
	fs       fs.FS
	path, id string
	decode   x.Decoder
	limit    int64
}

func Name(id string) Option   { return func(s *Source) { s.id = id } }
func MaxBytes(n int64) Option { return func(s *Source) { s.limit = n } }

// New reopens path on each Read. Use xconf.Poll for mutable filesystems.
// The source never closes the caller-owned filesystem.
func New(fsys fs.FS, path string, decode x.Decoder, opts ...Option) *Source {
	s := &Source{fs: fsys, path: path, id: "fs:" + path, decode: decode, limit: document.DefaultMaxBytes}
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
	if s.fs == nil || !fs.ValidPath(s.path) {
		return x.Layer{}, errors.New("invalid filesystem source")
	}
	f, err := s.fs.Open(s.path)
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
	return document.Decode(data, s.decode, s.id, "")
}
