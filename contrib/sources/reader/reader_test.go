package reader_test

import (
	"context"
	"errors"
	json "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/reader"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepeatableOwnedFactoryAndCapture(t *testing.T) {
	var content atomic.Value
	content.Store(`{"n":1}`)
	var closes atomic.Int64
	s := source.New("reader", func(context.Context) (io.ReadCloser, error) {
		return &tracked{Reader: strings.NewReader(content.Load().(string)), close: func() error { closes.Add(1); return nil }}, nil
	}, json.Decode)
	a, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	content.Store(`{"n":2}`)
	b, err := s.Read(context.Background(), nil)
	if err != nil || a.Revision == b.Revision || closes.Load() != 2 {
		t.Fatal(a, b, err, closes.Load())
	}
	captured, err := source.FromReader(context.Background(), "captured", strings.NewReader(`{"n":3}`), json.Decode)
	if err != nil {
		t.Fatal(err)
	}
	a, err = captured.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Values["n"] = "mutated"
	b, err = captured.Read(context.Background(), nil)
	if err != nil || b.Values["n"] == "mutated" {
		t.Fatal(b, err)
	}
	if _, err = source.FromReader(context.Background(), "large", strings.NewReader("123"), json.Decode, source.MaxBytes(2)); err == nil {
		t.Fatal("unbounded capture")
	}
}

type tracked struct {
	io.Reader
	close func() error
}

func (r *tracked) Close() error { return r.close() }
func TestCancellationUnblocksReadAndClosesOnce(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	var closes atomic.Int64
	started := make(chan struct{})
	s := source.New("pipe", func(context.Context) (io.ReadCloser, error) {
		close(started)
		return &tracked{Reader: r, close: func() error { closes.Add(1); return r.Close() }}, nil
	}, json.Decode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Read(ctx, nil); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || closes.Load() != 1 {
			t.Fatal(err, closes.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("reader not interrupted")
	}
}
func TestCloseAndOpenErrors(t *testing.T) {
	want := errors.New("close failed")
	s := source.New("reader", func(context.Context) (io.ReadCloser, error) {
		return &tracked{Reader: strings.NewReader(`{}`), close: func() error { return want }}, nil
	}, json.Decode)
	if _, err := s.Read(context.Background(), nil); !errors.Is(err, want) {
		t.Fatal(err)
	}
	var closed bool
	s = source.New("reader", func(context.Context) (io.ReadCloser, error) {
		return &tracked{Reader: strings.NewReader(""), close: func() error { closed = true; return nil }}, want
	}, json.Decode)
	if _, err := s.Read(context.Background(), nil); !errors.Is(err, want) || !closed {
		t.Fatal(err, closed)
	}
}
