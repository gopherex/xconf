// Package nats watches a document stored in one NATS JetStream KV key.
package nats

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
	"github.com/nats-io/nats.go/jetstream"
)

type Bucket interface {
	Bucket() string
	Get(context.Context, string) (jetstream.KeyValueEntry, error)
	Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}
type Option func(*Source)
type Source struct {
	bucket            Bucket
	key, id           string
	decode            x.Decoder
	limit             int64
	timeout, interval time.Duration
}

func Name(id string) Option          { return func(s *Source) { s.id = id } }
func MaxBytes(n int64) Option        { return func(s *Source) { s.limit = n } }
func Timeout(d time.Duration) Option { return func(s *Source) { s.timeout = d } }

// Interval sets reconciliation polling in addition to native watch, so outages
// and missed notifications are eventually detected. Zero disables reconciliation.
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }

// New uses an existing bucket. It never closes the caller's NATS connection.
// Each key contains one complete document; wildcards are not accepted.
func New(bucket Bucket, key string, decode x.Decoder, opts ...Option) *Source {
	id := "nats:" + key
	if bucket != nil {
		id = "nats:" + bucket.Bucket() + "/" + key
	}
	s := &Source{bucket: bucket, key: key, id: id, decode: decode, limit: document.DefaultMaxBytes, timeout: 10 * time.Second, interval: 30 * time.Second}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) valid() bool {
	return s.bucket != nil && s.key != "" && !strings.ContainsAny(s.key, "*> \t\r\n") && s.decode != nil && s.limit > 0 && s.timeout > 0 && s.interval >= 0
}
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if !s.valid() {
		return x.Layer{}, errors.New("invalid NATS source settings")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	entry, err := s.bucket.Get(ctx, s.key)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return x.Layer{}, x.ErrNotFound
	}
	if err != nil {
		return x.Layer{}, err
	}
	if entry == nil {
		return x.Layer{}, errors.New("empty NATS response")
	}
	if entry.Operation() != jetstream.KeyValuePut {
		return x.Layer{}, x.ErrNotFound
	}
	data := entry.Value()
	if int64(len(data)) > s.limit {
		return x.Layer{}, errors.New("document exceeds size limit")
	}
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	return document.Decode(data, s.decode, s.id, strconv.FormatUint(entry.Revision(), 10))
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if !s.valid() {
		return nil, errors.New("invalid NATS source settings")
	}
	ctx, cancel := context.WithCancel(ctx)
	// Register synchronously before the runtime's initial Read; include deletes.
	watcher, err := s.bucket.Watch(ctx, s.key, jetstream.MetaOnly())
	if err != nil {
		if watcher != nil {
			_ = watcher.Stop()
		}
		cancel()
		return nil, err
	}
	if watcher == nil {
		cancel()
		return nil, errors.New("nil NATS watcher")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if watcher != nil {
				_ = watcher.Stop()
				// Release SDK callbacks that may be waiting to enqueue metadata.
				// The SDK closes Updates after its last callback returns.
				for range watcher.Updates() {
				}
			}
		}()
		var tick <-chan time.Time
		if s.interval > 0 {
			t := time.NewTicker(s.interval)
			defer t.Stop()
			tick = t.C
		}
		backoff := 250 * time.Millisecond
		for {
			if watcher == nil {
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				var err error
				watcher, err = s.bucket.Watch(ctx, s.key, jetstream.MetaOnly())
				if err != nil || watcher == nil {
					if watcher != nil {
						_ = watcher.Stop()
						watcher = nil
					}
					notify()
					backoff = min(2*backoff, 5*time.Second)
					continue
				}
				backoff = 250 * time.Millisecond
				notify()
			}
			select {
			case <-ctx.Done():
				return
			case <-tick:
				notify()
			case entry, ok := <-watcher.Updates():
				if !ok {
					_ = watcher.Stop()
					watcher = nil
					notify()
					continue
				}
				if entry != nil {
					notify()
				}
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}
