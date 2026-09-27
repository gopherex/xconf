// Package reader loads documents from repeatable reader factories or captured streams.
package reader

import (
	"bytes"
	"context"
	"errors"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
	"io"
	"sync"
)

// Opener returns a fresh owned reader on every call. It must honor ctx; the
// returned reader's Close must be safe during Read and unblock a pending Read.
type Opener func(context.Context) (io.ReadCloser, error)
type Option func(*Source)
type Source struct {
	id     string
	open   Opener
	decode x.Decoder
	limit  int64
}

func MaxBytes(n int64) Option { return func(s *Source) { s.limit = n } }
func New(name string, open Opener, decode x.Decoder, opts ...Option) *Source {
	s := &Source{id: name, open: open, decode: decode, limit: document.DefaultMaxBytes}
	for _, o := range opts {
		o(s)
	}
	return s
}

// FromReader captures a borrowed stream once, without closing it. The caller
// must arrange cancellation of blocking reads. Subsequent reloads use the capture.
func FromReader(ctx context.Context, name string, r io.Reader, decode x.Decoder, opts ...Option) (*Source, error) {
	s := New(name, nil, decode, opts...)
	if r == nil || decode == nil {
		return nil, errors.New("nil reader or decoder")
	}
	data, err := document.Read(ctx, r, s.limit)
	if err != nil {
		return nil, err
	}
	s.open = func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return s, nil
}
func (s *Source) Name() string { return s.id }
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if s.open == nil || s.decode == nil || s.limit <= 0 {
		return x.Layer{}, errors.New("invalid reader source settings")
	}
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	r, err := s.open(ctx)
	if err != nil {
		if r != nil {
			_ = r.Close()
		}
		return x.Layer{}, err
	}
	if r == nil {
		return x.Layer{}, errors.New("opener returned nil reader")
	}
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = r.Close() }) }
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { closeReader(); close(finished) })
	data, readErr := document.Read(ctx, r, s.limit)
	if !stop() {
		<-finished
	}
	closeReader()
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	if readErr != nil {
		return x.Layer{}, readErr
	}
	if closeErr != nil {
		return x.Layer{}, closeErr
	}
	return document.Decode(data, s.decode, s.id, "")
}
