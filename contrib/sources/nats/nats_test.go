package nats_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	json "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/nats"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestLiveJetStreamWatchRollbackDeletePurge(t *testing.T) {
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	defer srv.WaitForShutdown()
	defer srv.Shutdown()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS not ready")
	}
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	bucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "CONFIG", Storage: jetstream.MemoryStorage})
	if err != nil {
		t.Fatal(err)
	}
	schema := sp.NewSchema(sp.ID("test", "nats", sp.Ver(1, 0, 0))).Fields(sp.Int64("n").Gte(1).Required()).MustBuild()
	low, _ := x.NewMemory("base", map[string]any{"n": 1})
	source := source.New(bucket, "app", json.Decode, source.Interval(0))
	r, err := x.Open(ctx, schema, low, x.Optional(source))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events := r.Subscribe(ctx)
	<-events
	wait := func(pred func(x.Event) bool) x.Event {
		t.Helper()
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		for {
			select {
			case e, ok := <-events:
				if !ok {
					t.Fatal("closed")
				}
				if pred(e) {
					return e
				}
			case <-timer.C:
				t.Fatal("event timeout")
			}
		}
	}
	is := func(n int64) func(x.Event) bool {
		return func(e x.Event) bool { v, _ := x.Decode[map[string]any](e.Snapshot); return e.Err == nil && v["n"] == n }
	}
	if _, err = bucket.Put(ctx, "app", []byte(`{"n":9007199254740993}`)); err != nil {
		t.Fatal(err)
	}
	e := wait(is(9007199254740993))
	version := e.Snapshot.Version()
	if _, err = bucket.Put(ctx, "app", []byte(`{"n":0}`)); err != nil {
		t.Fatal(err)
	}
	e = wait(func(e x.Event) bool { return e.Err != nil })
	if e.Snapshot.Version() != version {
		t.Fatal("bad value published")
	}
	if err = bucket.Delete(ctx, "app"); err != nil {
		t.Fatal(err)
	}
	wait(is(1))
	if _, err = bucket.Put(ctx, "app", []byte(`{"n":3}`)); err != nil {
		t.Fatal(err)
	}
	wait(is(3))
	if err = bucket.Purge(ctx, "app"); err != nil {
		t.Fatal(err)
	}
	wait(is(1))
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = bucket.Put(ctx, "app", []byte(`{"n":4}`)); err != nil {
		t.Fatal("caller connection closed", err)
	}
}

type watcher struct {
	updates chan jetstream.KeyValueEntry
	once    sync.Once
}

func (w *watcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *watcher) Stop() error                             { w.once.Do(func() { close(w.updates) }); return nil }

type reconnectBucket struct {
	mu       sync.Mutex
	watchers []*watcher
	calls    int
}

func (*reconnectBucket) Bucket() string { return "test" }
func (*reconnectBucket) Get(context.Context, string) (jetstream.KeyValueEntry, error) {
	return nil, jetstream.ErrKeyNotFound
}
func (b *reconnectBucket) Watch(ctx context.Context, _ string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.calls == 2 {
		return nil, errors.New("temporary outage")
	}
	w := &watcher{updates: make(chan jetstream.KeyValueEntry)}
	b.watchers = append(b.watchers, w)
	return w, nil
}
func TestClosedWatcherRetriesAndStops(t *testing.T) {
	bucket := &reconnectBucket{}
	s := source.New(bucket, "key", json.Decode, source.Interval(0))
	stop, err := s.Watch(context.Background(), func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	bucket.mu.Lock()
	first := bucket.watchers[0]
	bucket.mu.Unlock()
	_ = first.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bucket.mu.Lock()
		count := len(bucket.watchers)
		bucket.mu.Unlock()
		if count == 2 {
			stop()
			bucket.mu.Lock()
			last := bucket.watchers[1]
			bucket.mu.Unlock()
			if _, ok := <-last.Updates(); ok {
				t.Fatal("watcher still open")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watch did not reconnect")
}
