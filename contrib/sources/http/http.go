// Package http reads configuration documents using conditional HTTP requests.
package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
)

type Client interface {
	Do(*http.Request) (*http.Response, error)
}
type Option func(*Source)
type Source struct {
	client            Client
	url, id           string
	decode            x.Decoder
	headers           http.Header
	interval, timeout time.Duration
	limit             int64
	gate              chan struct{}
	cached            []byte
	etag, modified    string
}

func Name(id string) Option           { return func(s *Source) { s.id = id } }
func Headers(h http.Header) Option    { return func(s *Source) { s.headers = h.Clone() } }
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }
func Timeout(d time.Duration) Option  { return func(s *Source) { s.timeout = d } }
func MaxBytes(n int64) Option         { return func(s *Source) { s.limit = n } }

// New uses a caller-owned client. A nil client uses http.DefaultClient.
// Interval(0) disables polling. Timeout must be positive.
func New(client Client, address string, decode x.Decoder, opts ...Option) *Source {
	id := "http"
	if u, err := url.Parse(address); err == nil {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		id = u.String()
	}
	if client == nil {
		client = http.DefaultClient
	}
	s := &Source{client: client, url: address, id: id, decode: decode, interval: 30 * time.Second, timeout: 10 * time.Second, limit: document.DefaultMaxBytes, gate: make(chan struct{}, 1)}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if s.timeout <= 0 || s.decode == nil {
		return x.Layer{}, errors.New("invalid HTTP source settings")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return x.Layer{}, ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return x.Layer{}, err
	}
	req.Header = s.headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	// Conditional headers belong to the cache, never to caller-provided headers.
	req.Header.Del("If-None-Match")
	req.Header.Del("If-Modified-Since")
	if s.cached != nil {
		if s.etag != "" {
			req.Header.Set("If-None-Match", s.etag)
		}
		if s.modified != "" {
			req.Header.Set("If-Modified-Since", s.modified)
		}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return x.Layer{}, err
	}
	if resp == nil || resp.Body == nil {
		return x.Layer{}, errors.New("empty HTTP response")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if s.cached == nil {
			return x.Layer{}, errors.New("HTTP 304 without cached document")
		}
		return document.Decode(s.cached, s.decode, s.id, "")
	}
	if resp.StatusCode == http.StatusNotFound {
		s.cached = nil
		s.etag = ""
		s.modified = ""
		return x.Layer{}, x.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return x.Layer{}, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	data, err := document.Read(ctx, resp.Body, s.limit)
	if err != nil {
		return x.Layer{}, err
	}
	layer, err := document.Decode(data, s.decode, s.id, "")
	if err != nil {
		return x.Layer{}, err
	}
	s.cached = append([]byte{}, data...)
	s.etag = resp.Header.Get("ETag")
	s.modified = resp.Header.Get("Last-Modified")
	return layer, nil
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if s.interval == 0 {
		return func() {}, nil
	}
	return x.Poll(s, s.interval).(x.Watcher).Watch(ctx, notify)
}
