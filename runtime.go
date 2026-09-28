package xconf

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	"google.golang.org/protobuf/proto"
)

// Event carries the current valid snapshot, even after a failed reload. Changed
// paths are relative to the previous published version, not the last event a
// subscriber read. Slow subscribers receive the latest event, dropping intermediates.
type Event struct {
	Snapshot *Snapshot
	Err      error
	Changed  []Path
}
type Runtime struct {
	loader      *loader
	ctx         context.Context
	cancel      context.CancelFunc
	gate        chan struct{}
	dirty       chan struct{}
	done        chan struct{}
	stops       []func()
	check       func(*Snapshot) error
	mu          sync.Mutex
	current     *Snapshot
	subscribers map[chan Event]struct{}
	closed      bool
	failed      bool
}

func Open(ctx context.Context, schema *sp.Schema, sources ...Source) (*Runtime, error) {
	return open(ctx, schema, nil, sources)
}
func open(ctx context.Context, schema *sp.Schema, check func(*Snapshot) error, sources []Source) (*Runtime, error) {
	l, err := newLoader(schema, sources)
	if err != nil {
		return nil, err
	}
	return openLoader(ctx, l, check)
}

// Open starts a runtime with this loader's schema and compile options.
func (l *Loader) Open(ctx context.Context, sources ...Source) (*Runtime, error) {
	bound, err := l.bind(sources)
	if err != nil {
		return nil, err
	}
	return openLoader(ctx, bound, nil)
}
func openLoader(ctx context.Context, l *loader, check func(*Snapshot) error) (*Runtime, error) {
	ctx, cancel := context.WithCancel(ctx)
	r := &Runtime{loader: l, ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), dirty: make(chan struct{}, 1), done: make(chan struct{}), check: check, subscribers: map[chan Event]struct{}{}}
	cleanup := func() {
		cancel()
		for i := len(r.stops) - 1; i >= 0; i-- {
			r.stops[i]()
		}
	}
	for _, s := range l.sources {
		if w, ok := s.(Watcher); ok {
			stop, e := w.Watch(ctx, r.invalidate)
			if e != nil {
				if stop != nil {
					stop()
				}
				cleanup()
				return nil, &SourceError{s.Name(), "watch", e}
			}
			if stop != nil {
				r.stops = append(r.stops, stop)
			}
		}
	}
	// A load blocked only while sources are degraded waits for their recovery.
	var held *DegradedError
	initial, err := r.build(ctx)
	for errors.As(err, &held) && ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-r.dirty:
			initial, err = r.build(ctx)
		}
	}
	if held != nil && ctx.Err() != nil {
		err = errors.Join(held, ctx.Err())
	} else if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		cleanup()
		return nil, err
	}
	initial.version = 1
	r.current = initial
	go r.run()
	return r, nil
}
func (r *Runtime) build(ctx context.Context) (*Snapshot, error) {
	s, err := r.loader.load(ctx)
	if err == nil && r.check != nil {
		err = withDegraded(r.check(s), s.degraded)
	}
	return s, err
}
func (r *Runtime) invalidate() {
	select {
	case r.dirty <- struct{}{}:
	default:
	}
}
func (r *Runtime) run() {
	defer close(r.done)
	defer func() {
		for i := len(r.stops) - 1; i >= 0; i-- {
			r.stops[i]()
		}
		r.mu.Lock()
		r.closed = true
		for ch := range r.subscribers {
			close(ch)
			delete(r.subscribers, ch)
		}
		r.mu.Unlock()
	}()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.dirty:
			timer := time.NewTimer(20 * time.Millisecond)
			select {
			case <-r.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case <-r.dirty:
			default:
			}
			_, _ = r.Reload(r.ctx)
		}
	}
}
func (r *Runtime) Snapshot() *Snapshot { r.mu.Lock(); defer r.mu.Unlock(); return r.current }

// Close cancels reads/watchers and waits for shutdown. Sources must honor context.
func (r *Runtime) Close() error {
	r.cancel()
	<-r.done
	r.gate <- struct{}{}
	<-r.gate
	return nil
}

// Subscribe immediately delivers the current snapshot. Cancel ctx to unsubscribe.
func (r *Runtime) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 1)
	r.mu.Lock()
	if r.closed || r.ctx.Err() != nil || ctx.Err() != nil {
		close(ch)
		r.mu.Unlock()
		return ch
	}
	r.subscribers[ch] = struct{}{}
	ch <- Event{Snapshot: r.current}
	r.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-r.ctx.Done():
		}
		r.mu.Lock()
		if _, ok := r.subscribers[ch]; ok {
			delete(r.subscribers, ch)
			close(ch)
		}
		r.mu.Unlock()
	}()
	return ch
}
func copyEvent(e Event) Event {
	out := e
	out.Changed = make([]Path, len(e.Changed))
	for i, p := range e.Changed {
		out.Changed[i] = append(Path(nil), p...)
	}
	out.Err = copyErr(e.Err)
	return out
}
func copyErr(err error) error {
	switch v := err.(type) {
	case *DegradedError:
		return &DegradedError{maps.Clone(v.Degraded), copyErr(v.Err)}
	case *ValidationError:
		return &ValidationError{Result: proto.Clone(v.Result).(*sp.ValidationResult), Report: proto.Clone(v.Report).(*sp.ResolveReport)}
	}
	return err
}
func (r *Runtime) publish(e Event) {
	for ch := range r.subscribers {
		select {
		case <-ch:
		default:
		}
		ch <- copyEvent(e)
	}
}

// Reload serializes full reads and keeps the last valid snapshot on every failure.
// A successful rebuild with identical values, provenance and degraded sources
// keeps its version; a source becoming stale or recovering publishes an event.
func (r *Runtime) Reload(ctx context.Context) (*Snapshot, error) {
	if r.ctx.Err() != nil {
		return r.Snapshot(), ErrClosed
	}
	select {
	case r.gate <- struct{}{}:
	case <-ctx.Done():
		return r.Snapshot(), ctx.Err()
	case <-r.ctx.Done():
		return r.Snapshot(), ErrClosed
	}
	defer func() { <-r.gate }()
	readCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	next, err := r.build(readCtx)
	if err == nil {
		err = readCtx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.ctx.Err() != nil {
		return r.current, ErrClosed
	}
	if err != nil {
		r.failed = true
		r.publish(Event{Snapshot: r.current, Err: err})
		return r.current, err
	}
	old := r.current
	if proto.Equal(old.baked.Values, next.baked.Values) && reflect.DeepEqual(old.origins, next.origins) && reflect.DeepEqual(old.revisions, next.revisions) && proto.Equal(old.validation, next.validation) && sameKeys(old.degraded, next.degraded) {
		if r.failed {
			r.failed = false
			r.publish(Event{Snapshot: old})
		}
		return old, nil
	}
	r.failed = false
	next.version = old.version + 1
	r.current = next
	r.publish(Event{Snapshot: next, Changed: changedPaths(old.baked.Values, next.baked.Values)})
	return next, nil
}

// sameKeys compares degraded sources, ignoring volatile error text.
func sameKeys(a, b map[string]error) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
func changedPaths(a, b *sp.StructValue) []Path {
	var out []Path
	var walk func(*sp.Value, *sp.Value, Path)
	walk = func(a, b *sp.Value, path Path) {
		if proto.Equal(a, b) {
			return
		}
		am, bm := a.GetStructValue(), b.GetStructValue()
		if am != nil && bm != nil {
			keys := map[string]bool{}
			for k := range am.Fields {
				keys[k] = true
			}
			for k := range bm.Fields {
				keys[k] = true
			}
			ordered := make([]string, 0, len(keys))
			for k := range keys {
				ordered = append(ordered, k)
			}
			sort.Strings(ordered)
			for _, k := range ordered {
				walk(am.Fields[k], bm.Fields[k], child(path, k))
			}
			return
		}
		al, bl := a.GetListValue(), b.GetListValue()
		if al != nil && bl != nil && len(al.Items) == len(bl.Items) {
			for i := range al.Items {
				walk(al.Items[i], bl.Items[i], child(path, strconv.Itoa(i)))
			}
			return
		}
		out = append(out, path)
	}
	walk(sp.StructV(a.Fields), sp.StructV(b.Fields), nil)
	return out
}

// TypedRuntime validates destination decoding before publishing each snapshot.
// Current returns a fresh value; callers can freely mutate its maps and slices.
type TypedRuntime[T any] struct{ *Runtime }

func OpenAs[T any](ctx context.Context, schema *sp.Schema, sources ...Source) (*TypedRuntime[T], error) {
	r, err := open(ctx, schema, func(s *Snapshot) error { _, err := Decode[T](s); return err }, sources)
	if err != nil {
		return nil, err
	}
	return &TypedRuntime[T]{r}, nil
}
func (r *TypedRuntime[T]) Current() (T, error) { return Decode[T](r.Snapshot()) }
