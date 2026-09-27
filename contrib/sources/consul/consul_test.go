package consul_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	decoder "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/consul"
	"github.com/hashicorp/consul/api"
)

func client(t *testing.T, server *httptest.Server) *api.Client {
	t.Helper()
	config := api.DefaultConfig()
	config.Address = server.URL
	config.HttpClient = server.Client()
	config.Token = "test-token"
	config.Datacenter = "dc-test"
	config.Namespace = "ns-test"
	config.Partition = "part-test"
	c, err := api.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSDKReadPrecisionErrorsLimitsAndOwnership(t *testing.T) {
	type response struct {
		status int
		value  string
	}
	var state atomic.Value
	state.Store(response{200, `{"n":9007199254740993}`})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/kv/apps/api" || r.Method != "GET" || q.Has("index") || q.Has("recurse") || !q.Has("consistent") || r.Header.Get("X-Consul-Token") != "test-token" || q.Get("dc") != "dc-test" || q.Get("ns") != "ns-test" || q.Get("partition") != "part-test" {
			t.Errorf("unexpected request %s", r.URL)
		}
		v := state.Load().(response)
		w.Header().Set("X-Consul-Index", "17")
		w.WriteHeader(v.status)
		if v.status == 200 {
			_ = json.NewEncoder(w).Encode([]api.KVPair{{Key: "apps/api", Value: []byte(v.value), ModifyIndex: 17}})
		} else {
			_, _ = w.Write([]byte("test provider error"))
		}
	}))
	defer server.Close()
	s := source.New(client(t, server).KV(), "apps/api", decoder.Decode)
	layer, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Revision != "17" || layer.Locations[""].Name != "consul:apps/api" || layer.Values["n"].(json.Number).String() != "9007199254740993" {
		t.Fatal(layer)
	}
	layer.Values["n"] = "changed"
	next, err := s.Read(context.Background(), nil)
	if err != nil || next.Values["n"] == "changed" {
		t.Fatal(next, err)
	}
	state.Store(response{404, ""})
	if _, err = s.Read(context.Background(), nil); !errors.Is(err, x.ErrNotFound) {
		t.Fatal(err)
	}
	for _, status := range []int{403, 500} {
		state.Store(response{status, ""})
		if _, err = x.Optional(s).Read(context.Background(), nil); err == nil || errors.Is(err, x.ErrNotFound) {
			t.Fatalf("status %d: %v", status, err)
		}
	}
	state.Store(response{200, `{`})
	if _, err = s.Read(context.Background(), nil); err == nil {
		t.Fatal("bad JSON accepted")
	}
	state.Store(response{200, `{"n":1}`})
	if _, err = source.New(client(t, server).KV(), "apps/api", decoder.Decode, source.MaxBytes(1)).Read(context.Background(), nil); err == nil {
		t.Fatal("limit ignored")
	}
}

// This endpoint models Consul's index blocking protocol using the actual SDK.
type kvServer struct {
	mu      sync.Mutex
	index   uint64
	status  int
	value   string
	changed chan struct{}
}

func newKVServer() *kvServer { return &kvServer{index: 1, status: 404, changed: make(chan struct{})} }
func (s *kvServer) set(status int, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index++
	s.status = status
	s.value = value
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *kvServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requested, _ := strconv.ParseUint(r.URL.Query().Get("index"), 10, 64)
	s.mu.Lock()
	index, status, value, changed := s.index, s.status, s.value, s.changed
	s.mu.Unlock()
	if requested > 0 && requested == index && status < 500 {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-timer.C:
		}
		s.mu.Lock()
		index, status, value = s.index, s.status, s.value
		s.mu.Unlock()
	}
	w.Header().Set("X-Consul-Index", strconv.FormatUint(index, 10))
	w.WriteHeader(status)
	if status == 200 {
		_ = json.NewEncoder(w).Encode([]api.KVPair{{Key: "apps/api", Value: []byte(value), ModifyIndex: index}})
	}
}
func TestRuntimeWatchCreateInvalidDeleteOutageRecovery(t *testing.T) {
	state := newKVServer()
	server := httptest.NewServer(state)
	defer server.Close()
	schema := sp.NewSchema(sp.ID("test", "consul", sp.Ver(1, 0, 0))).Fields(sp.Int64("n").Gte(1).Required()).MustBuild()
	base, _ := x.NewMemory("base", map[string]any{"n": 1})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := client(t, server)
	r, err := x.Open(ctx, schema, base, x.Optional(source.New(c.KV(), "apps/api", decoder.Decode, source.WaitTime(100*time.Millisecond))))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events := r.Subscribe(ctx)
	<-events
	wait := func(pred func(x.Event) bool) x.Event {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case e, ok := <-events:
				if !ok {
					t.Fatal("events closed")
				}
				if pred(e) {
					return e
				}
			case <-timer.C:
				t.Fatal("watch event timeout")
			}
		}
	}
	is := func(n int64) func(x.Event) bool {
		return func(e x.Event) bool { v, _ := x.Decode[map[string]any](e.Snapshot); return e.Err == nil && v["n"] == n }
	}
	state.set(200, `{"n":2}`)
	e := wait(is(2))
	version := e.Snapshot.Version()
	state.set(200, `{"n":0}`)
	e = wait(func(e x.Event) bool { return e.Err != nil })
	if e.Snapshot.Version() != version {
		t.Fatal("invalid configuration published")
	}
	state.set(404, "")
	wait(is(1))
	state.set(503, "")
	e = wait(func(e x.Event) bool { return e.Err != nil })
	if !is(1)(x.Event{Snapshot: e.Snapshot}) {
		t.Fatal("outage replaced snapshot")
	}
	state.set(200, `{"n":3}`)
	wait(is(3))
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	// The caller-owned client remains usable after watchers have stopped.
	if _, err = source.New(c.KV(), "apps/api", decoder.Decode).Read(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

type getFunc func(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)

func (f getFunc) Get(key string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
	return f(key, q)
}

func TestIndexRegressionZeroAndCancellation(t *testing.T) {
	requests := make(chan uint64, 10)
	calls := 0
	backend := getFunc(func(_ string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
		requests <- q.WaitIndex
		calls++
		indexes := []uint64{100, 50, 50, 0, 0}
		if calls <= len(indexes) {
			return nil, &api.QueryMeta{LastIndex: indexes[calls-1]}, nil
		}
		<-q.Context().Done()
		return nil, nil, q.Context().Err()
	})
	s := source.New(backend, "key", decoder.Decode)
	stop, err := s.Watch(context.Background(), func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for _, want := range []uint64{0, 100, 0, 50, 0, 1} {
		select {
		case got := <-requests:
			if got != want {
				t.Fatalf("index %d want %d", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("query timeout")
		}
	}
	done := make(chan struct{})
	go func() { stop(); stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel blocking query")
	}
}

func TestWatchBaselineCoversInitialReadGap(t *testing.T) {
	var mu sync.Mutex
	baseline := false
	backend := getFunc(func(_ string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
		mu.Lock()
		first := !baseline
		baseline = true
		mu.Unlock()
		if first {
			return nil, &api.QueryMeta{LastIndex: 1}, nil
		}
		// The key was created after the baseline, before the first blocking query.
		if q.WaitIndex < 2 {
			return &api.KVPair{Value: []byte(`{"n":2}`), ModifyIndex: 2}, &api.QueryMeta{LastIndex: 2}, nil
		}
		<-q.Context().Done()
		return nil, nil, q.Context().Err()
	})
	s := source.New(backend, "key", decoder.Decode)
	notifications := make(chan struct{}, 1)
	stop, err := s.Watch(context.Background(), func() {
		select {
		case notifications <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-notifications:
	case <-time.After(time.Second):
		t.Fatal("update in startup gap missed")
	}
	l, err := s.Read(context.Background(), nil)
	if err != nil || l.Revision != "2" {
		t.Fatal(l, err)
	}
}

func TestWatchNotifiesRecoveryAtSameIndex(t *testing.T) {
	calls := 0
	backend := getFunc(func(_ string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
		calls++
		if calls == 2 {
			return nil, nil, errors.New("transient failure")
		}
		if calls > 3 {
			<-q.Context().Done()
			return nil, nil, q.Context().Err()
		}
		return nil, &api.QueryMeta{LastIndex: 5}, nil
	})
	notifications := make(chan struct{}, 4)
	stop, err := source.New(backend, "key", decoder.Decode).Watch(context.Background(), func() { notifications <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for i := 0; i < 2; i++ {
		select {
		case <-notifications:
		case <-time.After(3 * time.Second):
			t.Fatal("error or recovery notification missing")
		}
	}
}

func TestTimeoutDisabledWatchAndInvalidSettings(t *testing.T) {
	backend := getFunc(func(_ string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
		<-q.Context().Done()
		return nil, nil, q.Context().Err()
	})
	s := source.New(backend, "key", decoder.Decode, source.Timeout(20*time.Millisecond))
	if _, err := s.Read(context.Background(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if stop, err := s.Watch(context.Background(), func() {}); !errors.Is(err, context.DeadlineExceeded) || stop != nil {
		t.Fatal("startup failure hidden", err)
	}
	s = source.New(backend, "key", decoder.Decode, source.WaitTime(0))
	stop, err := s.Watch(context.Background(), func() { t.Error("disabled watch notified") })
	if err != nil {
		t.Fatal(err)
	}
	stop()
	for _, s := range []*source.Source{source.New(nil, "key", decoder.Decode), source.New(backend, "", decoder.Decode), source.New(backend, "key", nil), source.New(backend, "key", decoder.Decode, source.WaitTime(11*time.Minute))} {
		if _, err = s.Read(context.Background(), nil); err == nil {
			t.Fatal("invalid settings accepted")
		}
	}
}

func TestSDKStopCancelsBlockingRequest(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("index") == "7" {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return
		}
		w.Header().Set("X-Consul-Index", "7")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	var notifications atomic.Int64
	stop, err := source.New(client(t, server).KV(), "apps/api", decoder.Decode).Watch(context.Background(), func() { notifications.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("no blocking request")
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop blocked")
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP request not canceled")
	}
	if notifications.Load() != 0 {
		t.Fatal("shutdown emitted a notification")
	}
}
