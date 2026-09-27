// Package etcd loads documents from one etcd v3 key and watches revisions.
package etcd

import (
	"context"
	"errors"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
	clientv3 "go.etcd.io/etcd/client/v3"
	"strconv"
	"time"
)

type Client interface {
	Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Watch(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
}
type Option func(*Source)
type Source struct {
	client            Client
	key, id           string
	decode            x.Decoder
	limit             int64
	timeout, interval time.Duration
}

func Name(id string) Option          { return func(s *Source) { s.id = id } }
func MaxBytes(n int64) Option        { return func(s *Source) { s.limit = n } }
func Timeout(d time.Duration) Option { return func(s *Source) { s.timeout = d } }

// Interval sets reconciliation polling in addition to native watch; zero disables it.
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }
func New(client Client, key string, decode x.Decoder, opts ...Option) *Source {
	s := &Source{client: client, key: key, id: "etcd:" + key, decode: decode, limit: document.DefaultMaxBytes, timeout: 10 * time.Second, interval: 30 * time.Second}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) valid() bool {
	return s.client != nil && s.key != "" && s.decode != nil && s.limit > 0 && s.timeout > 0 && s.interval >= 0
}
func (s *Source) get(ctx context.Context) (*clientv3.GetResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	r, err := s.client.Get(ctx, s.key)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.Header == nil || r.Header.Revision <= 0 || len(r.Kvs) > 1 {
		return nil, errors.New("invalid etcd response")
	}
	return r, nil
}
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if !s.valid() {
		return x.Layer{}, errors.New("invalid etcd source settings")
	}
	r, err := s.get(ctx)
	if err != nil {
		return x.Layer{}, err
	}
	if len(r.Kvs) == 0 {
		return x.Layer{}, x.ErrNotFound
	}
	kv := r.Kvs[0]
	if kv == nil || string(kv.Key) != s.key {
		return x.Layer{}, errors.New("invalid etcd KV entry")
	}
	if int64(len(kv.Value)) > s.limit {
		return x.Layer{}, errors.New("document exceeds size limit")
	}
	return document.Decode(kv.Value, s.decode, s.id, strconv.FormatInt(kv.ModRevision, 10))
}

// stream waits for the server's creation acknowledgement. Its timeout only
// bounds registration; a successful watch keeps its context until canceled.
func (s *Source) stream(ctx context.Context, revision int64) (clientv3.WatchChan, context.CancelFunc, error) {
	child, cancel := context.WithCancel(clientv3.WithRequireLeader(ctx))
	timer := time.AfterFunc(s.timeout, cancel)
	ch := s.client.Watch(child, s.key, clientv3.WithRev(revision+1), clientv3.WithCreatedNotify(), clientv3.WithProgressNotify())
	select {
	case <-child.Done():
		timer.Stop()
		cancel()
		return nil, nil, errors.New("etcd watch registration canceled or timed out")
	case response, ok := <-ch:
		timer.Stop()
		if err := child.Err(); err != nil {
			cancel()
			return nil, nil, err
		}
		if !ok {
			cancel()
			return nil, nil, errors.New("etcd watch closed during registration")
		}
		if err := response.Err(); err != nil {
			cancel()
			return nil, nil, err
		}
		if !response.Created {
			cancel()
			return nil, nil, errors.New("etcd watch missing creation acknowledgement")
		}
		return ch, cancel, nil
	}
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if !s.valid() {
		return nil, errors.New("invalid etcd source settings")
	}
	ctx, cancel := context.WithCancel(ctx)
	r, err := s.get(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	ch, stopStream, err := s.stream(ctx, r.Header.Revision)
	if err != nil {
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if stopStream != nil {
				stopStream()
			}
		}()
		var ticks <-chan time.Time
		if s.interval > 0 {
			t := time.NewTicker(s.interval)
			defer t.Stop()
			ticks = t.C
		}
		backoff := 250 * time.Millisecond
		for {
			if ch == nil {
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				r, err := s.get(ctx)
				if err == nil {
					ch, stopStream, err = s.stream(ctx, r.Header.Revision)
				}
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					notify()
					backoff = min(backoff*2, 5*time.Second)
					continue
				}
				// A full runtime reload covers events lost to compaction or reconnection.
				backoff = 250 * time.Millisecond
				notify()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticks:
				notify()
			case response, ok := <-ch:
				if !ok || response.Err() != nil || response.Canceled {
					stopStream()
					stopStream = nil
					ch = nil
					notify()
					continue
				}
				if len(response.Events) > 0 {
					notify()
				}
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}
