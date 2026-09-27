package xconf_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
)

func schema(fields ...sp.FieldDef) *sp.Schema {
	return sp.NewSchema(sp.ID("test", "config", sp.Ver(1, 0, 0))).Coerce().Fields(fields...).MustBuild()
}
func memory(t *testing.T, name string, values map[string]any) *x.Memory {
	t.Helper()
	m, e := x.NewMemory(name, values)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func load(t *testing.T, s *sp.Schema, sources ...x.Source) *x.Snapshot {
	t.Helper()
	v, e := x.Load(context.Background(), s, sources...)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func value(t *testing.T, s *x.Snapshot) map[string]any {
	t.Helper()
	v, e := x.Decode[map[string]any](s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func event(t *testing.T, ch <-chan x.Event) x.Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("closed subscription")
		}
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("event timeout")
		return x.Event{}
	}
}
func TestLayeringPresenceOwnership(t *testing.T) {
	s := schema(sp.Object("db", sp.Str("host").Required(), sp.Int64("port").Default(5432)), sp.Bool("on").Default(true), sp.Str("text").Default("default"), sp.List("ports", sp.Int64("")), sp.Int64("big"))
	low := memory(t, "file", map[string]any{"db": map[string]any{"host": "localhost", "port": 1111}, "on": true, "text": "old", "ports": []any{1, 2}})
	upper := memory(t, "env", map[string]any{"db": map[string]any{"port": "0"}, "on": false, "text": "", "ports": []any{}, "big": json.Number("9007199254740993")})
	got := load(t, s, low, upper)
	v := value(t, got)
	if v["on"] != false || v["text"] != "" || len(v["ports"].([]any)) != 0 || v["big"] != int64(9007199254740993) {
		t.Fatalf("presence: %#v", v)
	}
	db := v["db"].(map[string]any)
	if db["host"] != "localhost" || db["port"] != int64(0) {
		t.Fatalf("db: %#v", db)
	}
	history := got.Explain("db", "port")
	if len(history) != 3 || history[0].Source != "file" || history[1].Source != "env" || history[2].Source != "schema" {
		t.Fatalf("origins: %+v", history)
	}
	db["host"] = "mutated"
	got.Baked().Values.Fields["db"] = sp.NullV()
	history[0].Source = "changed"
	if value(t, got)["db"].(map[string]any)["host"] != "localhost" || got.Explain("db", "port")[0].Source != "file" {
		t.Fatal("snapshot alias")
	}
	raw, _ := low.Read(context.Background(), s)
	if raw.Values["db"].(map[string]any)["port"] != int64(1111) {
		t.Fatal("source mutated")
	}
}
func TestEditsDefaultsNullAndUnion(t *testing.T) {
	s := schema(sp.Int64("n").Default(7), sp.Object("db", sp.Int64("port").Default(5432)).DefaultEmpty().Nullable(), sp.MapOf("labels", sp.Str("value")), sp.OneOf("job", "kind").Variant("a", sp.Str("kind"), sp.Str("old")).Variant("b", sp.Str("kind"), sp.Str("new")))
	a := memory(t, "a", map[string]any{"n": 9, "labels": map[string]any{"a": "b"}, "job": map[string]any{"kind": "a", "old": "a"}})
	b := memory(t, "b", nil)
	if err := b.Set(x.Layer{Values: map[string]any{"db": nil, "job": map[string]any{"kind": "b", "new": "b"}}, Edits: []x.Edit{{Kind: x.Delete, Path: x.Path{"n"}}, {Kind: x.Replace, Path: x.Path{"labels"}, Value: map[string]any{}}}}); err != nil {
		t.Fatal(err)
	}
	got := value(t, load(t, s, a, b))
	if got["n"] != int64(7) || got["db"] != nil || len(got["labels"].(map[string]any)) != 0 {
		t.Fatal(got)
	}
	if _, ok := got["job"].(map[string]any)["old"]; ok {
		t.Fatal("old variant leaked")
	}
	empty := value(t, load(t, s))
	if empty["db"].(map[string]any)["port"] != int64(5432) {
		t.Fatal(empty)
	}
}
func TestNoPrematureValidationOrNormalize(t *testing.T) {
	s := schema(sp.Int64("a").Required().Normalize("this + 1"), sp.Int64("b").Required(), sp.Computed("sum", "root.a + root.b").Result(sp.ResultInt64))
	a := memory(t, "a", map[string]any{"a": "bad"})
	b := memory(t, "b", map[string]any{"a": "3", "b": "4"})
	got := value(t, load(t, s, a, b))
	if got["a"] != int64(4) || got["sum"] != int64(8) {
		t.Fatal(got)
	}
}
func TestOptionalAndErrors(t *testing.T) {
	s := schema(sp.Str("token").Required().Secret())
	missing := x.SourceFunc{ID: "optional", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) { return x.Layer{}, x.ErrNotFound }}
	good := memory(t, "good", map[string]any{"token": "secret"})
	load(t, s, x.Optional(missing), good)
	cause := errors.New("sensitive provider body")
	bad := x.SourceFunc{ID: "bad", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) { return x.Layer{}, cause }}
	_, err := x.Load(context.Background(), s, x.Optional(bad), good)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "sensitive") {
		t.Fatal(err)
	}
	if _, err = x.Load(context.Background(), s, good, good); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, err = x.Load(context.Background(), s); err == nil {
		t.Fatal("required ignored")
	}
}
func TestReloadRollbackRecoveryAndRemoval(t *testing.T) {
	s := schema(sp.Int64("n").Gte(1).Required())
	low := memory(t, "low", map[string]any{"n": 2})
	high := memory(t, "high", map[string]any{"n": 3})
	r, err := x.OpenAs[struct {
		N int64 `json:"n"`
	}](context.Background(), s, low, high)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ch := r.Subscribe(context.Background())
	initial := event(t, ch)
	if err = high.Set(x.Layer{Values: map[string]any{"n": 0}}); err != nil {
		t.Fatal(err)
	}
	failed := event(t, ch)
	if failed.Err == nil || failed.Snapshot.Version() != initial.Snapshot.Version() {
		t.Fatal(failed)
	}
	if err = high.Set(x.Layer{Values: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	recovered := event(t, ch)
	cfg, err := r.Current()
	if err != nil || cfg.N != 2 || recovered.Err != nil || recovered.Snapshot.Version() != 2 {
		t.Fatalf("%+v %+v %v", cfg, recovered, err)
	}
	if !reflect.DeepEqual(recovered.Changed, []x.Path{{"n"}}) {
		t.Fatal(recovered.Changed)
	}
	v, err := r.Reload(context.Background())
	if err != nil || v.Version() != 2 {
		t.Fatal(v, err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Reload(context.Background()); !errors.Is(err, x.ErrClosed) {
		t.Fatal(err)
	}
}
func TestDecodeFailureRetainsSnapshot(t *testing.T) {
	s := schema(sp.Int64("n"))
	m := memory(t, "memory", map[string]any{"n": 1})
	r, err := x.OpenAs[struct {
		N int8 `json:"n"`
	}](context.Background(), s, m)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ch := r.Subscribe(context.Background())
	event(t, ch)
	_ = m.Set(x.Layer{Values: map[string]any{"n": 128}})
	e := event(t, ch)
	if e.Err == nil || e.Snapshot.Version() != 1 {
		t.Fatal(e)
	}
	cfg, err := r.Current()
	if err != nil || cfg.N != 1 {
		t.Fatal(cfg, err)
	}
}
func TestRecoveryEventForUnchangedSnapshot(t *testing.T) {
	var fail atomic.Bool
	src := x.SourceFunc{ID: "reader", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) {
		if fail.Load() {
			return x.Layer{}, errors.New("offline")
		}
		return x.Layer{Values: map[string]any{"n": 1}}, nil
	}}
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), src)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ch := r.Subscribe(context.Background())
	event(t, ch)
	fail.Store(true)
	_, _ = r.Reload(context.Background())
	if event(t, ch).Err == nil {
		t.Fatal("missing failure")
	}
	fail.Store(false)
	_, err = r.Reload(context.Background())
	e := event(t, ch)
	if err != nil || e.Err != nil || e.Snapshot.Version() != 1 {
		t.Fatal(e, err)
	}
}
func TestConcurrentReloadAndSlowSubscriber(t *testing.T) {
	var active, max atomic.Int64
	src := x.SourceFunc{ID: "reader", ReadFunc: func(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := max.Load()
			if n <= old || max.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return x.Layer{}, ctx.Err()
		case <-time.After(time.Millisecond):
		}
		return x.Layer{Values: map[string]any{"n": 1}}, nil
	}}
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), src)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.Subscribe(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Reload(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if max.Load() != 1 {
		t.Fatalf("overlapping reads: %d", max.Load())
	}
}

type registered struct {
	watch   atomic.Bool
	stopped atomic.Bool
	reads   atomic.Int64
	notify  func()
}

func (s *registered) Name() string { return "registered" }
func (s *registered) Watch(_ context.Context, notify func()) (func(), error) {
	s.notify = notify
	s.watch.Store(true)
	return func() { s.stopped.Store(true) }, nil
}
func (s *registered) Read(_ context.Context, _ *sp.Schema) (x.Layer, error) {
	if !s.watch.Load() {
		return x.Layer{}, errors.New("read before watch")
	}
	n := s.reads.Add(1)
	if n == 1 {
		s.notify()
	}
	return x.Layer{Values: map[string]any{"n": n}}, nil
}
func TestWatchRegistrationAndInitialInvalidation(t *testing.T) {
	s := &registered{}
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), s)
	if err != nil {
		t.Fatal(err)
	}
	ch := r.Subscribe(context.Background())
	e := event(t, ch)
	if e.Snapshot.Version() == 1 {
		e = event(t, ch)
	}
	if value(t, e.Snapshot)["n"] != int64(2) {
		t.Fatal(e)
	}
	_ = r.Close()
	if !s.stopped.Load() {
		t.Fatal("watch not stopped")
	}
}
func TestCloseCancelsInFlightRead(t *testing.T) {
	var reads atomic.Int64
	started := make(chan struct{})
	src := x.SourceFunc{ID: "blocking", ReadFunc: func(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
		if reads.Add(1) > 1 {
			close(started)
			<-ctx.Done()
			return x.Layer{}, ctx.Err()
		}
		return x.Layer{Values: map[string]any{"n": 1}}, nil
	}}
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), src)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { defer close(finished); _, _ = r.Reload(context.Background()) }()
	<-started
	done := make(chan struct{})
	go func() { _ = r.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close hung")
	}
	<-finished
}

func TestCompiledLoaderOwnsSchemaAndSourceCopies(t *testing.T) {
	s := schema(sp.Int64("n").Default(7))
	l, err := x.New(s, sp.WithCostLimit(10000))
	if err != nil {
		t.Fatal(err)
	}
	*s.Fields[0].GetInt64().Default = 99
	source := x.SourceFunc{ID: "isolated", ReadFunc: func(_ context.Context, sc *sp.Schema) (x.Layer, error) {
		*sc.Fields[0].GetInt64().Default = 42
		return x.Layer{}, nil
	}}
	a, err := l.Load(context.Background(), source)
	if err != nil || value(t, a)["n"] != int64(7) {
		t.Fatal(a, err)
	}
	r, err := l.Open(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if value(t, r.Snapshot())["n"] != int64(7) {
		t.Fatal("engine schema changed")
	}
}
func TestInvalidInitialSnapshotStopsWatchers(t *testing.T) {
	src := &registered{}
	_, err := x.Open(context.Background(), schema(sp.Str("required").Required()), src)
	if err == nil || !src.stopped.Load() {
		t.Fatal("initial failure leaked watcher", err)
	}
}
func TestLiteralMapPathsAndLocationInheritance(t *testing.T) {
	src := x.SourceFunc{ID: "env", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) {
		return x.Layer{Values: map[string]any{"values": map[string]any{"a.b/c": "7"}}, Locations: map[string]x.Location{"/values": {Name: "APP_VALUES"}}}, nil
	}}
	s := load(t, schema(sp.MapOf("values", sp.Int64("value"))), src)
	history := s.Explain("values", "a.b/c")
	if history[0].Location.Name != "APP_VALUES" || (x.Path{"values", "a.b/c"}).String() != "/values/a.b~1c" {
		t.Fatal(history)
	}
}
func TestSlowSubscriberGetsLatestAndCancels(t *testing.T) {
	var n atomic.Int64
	n.Store(1)
	src := x.SourceFunc{ID: "values", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) {
		return x.Layer{Values: map[string]any{"n": n.Load()}}, nil
	}}
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), src)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := r.Subscribe(ctx)
	for i := int64(2); i <= 5; i++ {
		n.Store(i)
		if _, err := r.Reload(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	e := event(t, ch)
	if e.Snapshot.Version() != 5 {
		t.Fatal(e.Snapshot.Version())
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(time.Second):
		t.Fatal("unsubscribe stuck")
	}
}
