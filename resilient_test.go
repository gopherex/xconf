package xconf_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
)

// flaky is a watchable source whose Read and Watch errors can be switched.
type flaky struct {
	mu                      sync.Mutex
	values                  map[string]any
	readErr, watchErr       error
	notify                  func()
	reads, watches, stopped atomic.Int64
}

func (f *flaky) Name() string { return "flaky" }
func (f *flaky) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return x.Layer{}, f.readErr
	}
	return x.Layer{Values: map[string]any{"n": f.values["n"]}, Revision: "r"}, ctx.Err()
}
func (f *flaky) Watch(_ context.Context, notify func()) (func(), error) {
	f.watches.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	f.notify = notify
	return func() { f.stopped.Add(1) }, nil
}
func (f *flaky) set(n any, readErr, watchErr error) {
	f.mu.Lock()
	f.values, f.readErr, f.watchErr = map[string]any{"n": n}, readErr, watchErr
	f.mu.Unlock()
}
func (f *flaky) changed() {
	f.mu.Lock()
	notify := f.notify
	f.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func resilient(f *flaky) x.Source {
	return x.Resilient(f, x.Backoff(5*time.Millisecond, 20*time.Millisecond))
}
func until(t *testing.T, ch <-chan x.Event, ok func(x.Event) bool) x.Event {
	t.Helper()
	for {
		if e := event(t, ch); ok(e) {
			return e
		}
	}
}
func quiet(t *testing.T, ch <-chan x.Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}
func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("condition timeout")
		}
	}
}

func TestResilientStartsDegradedAndRecovers(t *testing.T) {
	down := errors.New("dial tcp: connection refused")
	f := &flaky{}
	f.set(1, down, nil)
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), resilient(f))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	degraded := r.Snapshot().Degraded()
	if len(degraded) != 1 || !errors.Is(degraded["flaky"], down) {
		t.Fatal(degraded)
	}
	delete(degraded, "flaky")
	if len(r.Snapshot().Degraded()) != 1 {
		t.Fatal("degraded alias")
	}
	ch := r.Subscribe(context.Background())
	event(t, ch)
	f.set(1, nil, nil)
	e := until(t, ch, func(e x.Event) bool { return len(e.Snapshot.Degraded()) == 0 })
	if e.Err != nil || value(t, e.Snapshot)["n"] != int64(1) || e.Snapshot.Version() != 2 {
		t.Fatal(e)
	}
}

func TestResilientKeepsLastGoodValues(t *testing.T) {
	down := errors.New("unavailable")
	f := &flaky{}
	f.set(1, nil, nil)
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), resilient(f))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ch := r.Subscribe(context.Background())
	event(t, ch)
	f.set(2, down, nil)
	f.changed()
	e := until(t, ch, func(e x.Event) bool { return len(e.Snapshot.Degraded()) > 0 })
	if e.Err != nil || !errors.Is(e.Snapshot.Degraded()["flaky"], down) || value(t, e.Snapshot)["n"] != int64(1) || e.Snapshot.Revisions()["flaky"] != "r" || len(e.Changed) != 0 {
		t.Fatal(e)
	}
	f.set(3, nil, nil)
	e = until(t, ch, func(e x.Event) bool { return len(e.Snapshot.Degraded()) == 0 })
	if value(t, e.Snapshot)["n"] != int64(3) {
		t.Fatal(e)
	}
}

func TestResilientRetriesWatchRegistration(t *testing.T) {
	f := &flaky{}
	f.set(1, nil, errors.New("watch unavailable"))
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), resilient(f))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if len(r.Snapshot().Degraded()) != 0 {
		t.Fatal(r.Snapshot().Degraded())
	}
	eventually(t, func() bool { return f.watches.Load() >= 3 })
	reads := f.reads.Load()
	f.set(1, nil, nil)
	eventually(t, func() bool { return f.reads.Load() > reads }) // registration notifies
	ch := r.Subscribe(context.Background())
	event(t, ch)
	f.set(2, nil, nil)
	f.changed()
	if e := event(t, ch); value(t, e.Snapshot)["n"] != int64(2) {
		t.Fatal(e)
	}
	if err = r.Close(); err != nil || f.stopped.Load() != 1 {
		t.Fatal(err, f.stopped.Load())
	}
}

func TestResilientStopReleasesGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	f := &flaky{}
	f.set(1, errors.New("down"), errors.New("down"))
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), resilient(f))
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.watches.Load() >= 2 })
	_ = r.Close()
	eventually(t, func() bool { return runtime.NumGoroutine() <= before })

	var notified atomic.Int64
	f.set(1, errors.New("down"), nil)
	src := resilient(f)
	stop, err := src.(x.Watcher).Watch(context.Background(), func() { notified.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	_, _ = src.Read(context.Background(), nil)
	eventually(t, func() bool { return notified.Load() >= 2 })
	stop()
	after := notified.Load()
	time.Sleep(50 * time.Millisecond)
	if notified.Load() != after || f.stopped.Load() != 1 {
		t.Fatal("loop outlived stop", notified.Load(), after, f.stopped.Load())
	}
	eventually(t, func() bool { return runtime.NumGoroutine() <= before })
	if _, err = x.Resilient(f, x.Backoff(0, time.Second)).(x.Watcher).Watch(context.Background(), func() {}); err == nil {
		t.Fatal("invalid backoff accepted")
	}
}

func TestResilientOptionalNotFound(t *testing.T) {
	missing := x.SourceFunc{ID: "missing", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) { return x.Layer{}, x.ErrNotFound }}
	got := load(t, schema(sp.Int64("n").Default(7)), x.Resilient(x.Optional(missing)))
	if len(got.Degraded()) != 0 || value(t, got)["n"] != int64(7) {
		t.Fatal(got.Degraded())
	}
	stale := load(t, schema(sp.Int64("n").Default(7)), x.Resilient(missing))
	if !errors.Is(stale.Degraded()["missing"], x.ErrNotFound) {
		t.Fatal(stale.Degraded())
	}
}

func TestResilientSameDegradedSetPublishesNothing(t *testing.T) {
	f := &flaky{}
	f.set(1, nil, nil)
	r, err := x.Open(context.Background(), schema(sp.Int64("n")), resilient(f))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ch := r.Subscribe(context.Background())
	event(t, ch)
	f.set(1, errors.New("timeout A"), nil)
	if _, err = r.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e := event(t, ch); len(e.Snapshot.Degraded()) != 1 || e.Snapshot.Version() != 2 {
		t.Fatal(e)
	}
	f.set(1, errors.New("timeout B"), nil)
	if s, err := r.Reload(context.Background()); err != nil || s.Version() != 2 {
		t.Fatal(s, err)
	}
	quiet(t, ch)
}
