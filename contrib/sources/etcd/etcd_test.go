package etcd_test

import (
	"context"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	decoder "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/etcd"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbeddedEtcdWatchRollbackDeleteAndOwnership(t *testing.T) {
	cfg := embed.NewConfig()
	cfg.Dir = t.TempDir()
	cfg.LogLevel = "error"
	endpoint, _ := url.Parse("http://127.0.0.1:0")
	cfg.ListenClientUrls = []url.URL{*endpoint}
	cfg.AdvertiseClientUrls = cfg.ListenClientUrls
	cfg.ListenPeerUrls = []url.URL{*endpoint}
	cfg.AdvertisePeerUrls = cfg.ListenPeerUrls
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)
	server, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	select {
	case <-server.Server.ReadyNotify():
	case <-time.After(15 * time.Second):
		t.Fatal("etcd not ready")
	}
	client, err := clientv3.New(clientv3.Config{Endpoints: []string{server.Clients[0].Addr().String()}, DialTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	schema := sp.NewSchema(sp.ID("test", "etcd", sp.Ver(1, 0, 0))).Fields(sp.Int64("n").Gte(1).Required()).MustBuild()
	low, _ := x.NewMemory("base", map[string]any{"n": 1})
	s := source.New(client, "/app/config", decoder.Decode, source.Interval(0))
	r, err := x.Open(ctx, schema, low, x.Optional(s))
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
				t.Fatal("watch timeout")
			}
		}
	}
	is := func(n int64) func(x.Event) bool {
		return func(e x.Event) bool { v, _ := x.Decode[map[string]any](e.Snapshot); return e.Err == nil && v["n"] == n }
	}
	if _, err = client.Put(ctx, "/app/config", `{"n":9007199254740993}`); err != nil {
		t.Fatal(err)
	}
	e := wait(is(9007199254740993))
	version := e.Snapshot.Version()
	if _, err = client.Put(ctx, "/app/config", `{"n":0}`); err != nil {
		t.Fatal(err)
	}
	e = wait(func(e x.Event) bool { return e.Err != nil })
	if e.Snapshot.Version() != version {
		t.Fatal("bad config published")
	}
	if _, err = client.Delete(ctx, "/app/config"); err != nil {
		t.Fatal(err)
	}
	wait(is(1))
	if _, err = client.Put(ctx, "/app/config", `{"n":4}`); err != nil {
		t.Fatal(err)
	}
	wait(is(4))
	if _, err = source.New(client, "/app/config", decoder.Decode, source.MaxBytes(1)).Read(ctx, nil); err == nil {
		t.Fatal("limit ignored")
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Put(ctx, "/app/config", `{"n":5}`); err != nil {
		t.Fatal("client closed", err)
	}
}

type backend struct {
	get   func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
	watch func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
}

func (b backend) Get(c context.Context, k string, o ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	return b.get(c, k, o...)
}
func (b backend) Watch(c context.Context, k string, o ...clientv3.OpOption) clientv3.WatchChan {
	return b.watch(c, k, o...)
}
func TestCompactionRelistsAndRegistersFromNewRevision(t *testing.T) {
	var gets atomic.Int64
	revisions := make(chan int64, 4)
	notifications := make(chan struct{}, 4)
	b := backend{get: func(ctx context.Context, _ string, _ ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return &clientv3.GetResponse{Header: &etcdserverpb.ResponseHeader{Revision: gets.Add(1) * 10}}, nil
	}}
	var watches atomic.Int64
	b.watch = func(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
		op := clientv3.OpGet(key, opts...)
		revisions <- op.Rev()
		ch := make(chan clientv3.WatchResponse, 2)
		ch <- clientv3.WatchResponse{Created: true}
		if watches.Add(1) == 1 {
			ch <- clientv3.WatchResponse{Canceled: true, CompactRevision: 15}
		}
		go func() { <-ctx.Done(); close(ch) }()
		return ch
	}
	s := source.New(b, "key", decoder.Decode, source.Interval(0))
	stop, err := s.Watch(context.Background(), func() { notifications <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for _, want := range []int64{11, 21} {
		select {
		case got := <-revisions:
			if got != want {
				t.Fatal(got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no re-registration")
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-notifications:
		case <-time.After(time.Second):
			t.Fatal("missing invalidation")
		}
	}
	stop()
}
func TestRegistrationTimeout(t *testing.T) {
	b := backend{get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return &clientv3.GetResponse{Header: &etcdserverpb.ResponseHeader{Revision: 1}}, nil
	}, watch: func(ctx context.Context, _ string, _ ...clientv3.OpOption) clientv3.WatchChan {
		ch := make(chan clientv3.WatchResponse)
		go func() { <-ctx.Done(); close(ch) }()
		return ch
	}}
	if stop, err := source.New(b, "key", decoder.Decode, source.Timeout(20*time.Millisecond)).Watch(context.Background(), func() {}); err == nil || stop != nil {
		t.Fatal("unbounded registration", err)
	}
}
