package xconf

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
)

// SourceFunc adapts a custom reader. Name must be stable and unique in a loader.
type SourceFunc struct {
	ID       string
	ReadFunc func(context.Context, *sp.Schema) (Layer, error)
}

func (s SourceFunc) Name() string { return s.ID }
func (s SourceFunc) Read(ctx context.Context, schema *sp.Schema) (Layer, error) {
	if s.ReadFunc == nil {
		return Layer{}, errors.New("nil source reader")
	}
	return s.ReadFunc(ctx, schema)
}

// Memory is a concurrent, watchable source useful for overrides and tests.
type Memory struct {
	mu       sync.Mutex
	id       string
	layer    Layer
	revision uint64
	watchers map[int]func()
	next     int
}

func NewMemory(name string, values map[string]any) (*Memory, error) {
	m := &Memory{id: name, watchers: map[int]func(){}}
	if err := m.Set(Layer{Values: values}); err != nil {
		return nil, err
	}
	return m, nil
}
func (m *Memory) Name() string { return m.id }
func cloneLayer(l Layer) (Layer, error) {
	out := l
	out.Locations = map[string]Location{}
	for k, v := range l.Locations {
		out.Locations[k] = v
	}
	out.Edits = make([]Edit, len(l.Edits))
	for i, e := range l.Edits {
		v, err := cloneInput(e.Value, 0)
		if err != nil {
			return Layer{}, err
		}
		out.Edits[i] = Edit{e.Kind, append(Path(nil), e.Path...), v}
	}
	if l.Values != nil {
		v, err := cloneInput(l.Values, 0)
		if err != nil {
			return Layer{}, err
		}
		out.Values = v.(map[string]any)
	}
	return out, nil
}
func (m *Memory) Set(layer Layer) error {
	l, err := cloneLayer(layer)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	if l.Revision == "" {
		l.Revision = fmt.Sprint(m.revision)
	}
	m.layer = l
	for _, notify := range m.watchers {
		notify()
	}
	return nil
}
func (m *Memory) Read(ctx context.Context, _ *sp.Schema) (Layer, error) {
	if err := ctx.Err(); err != nil {
		return Layer{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneLayer(m.layer)
}
func (m *Memory) Watch(_ context.Context, notify func()) (func(), error) {
	m.mu.Lock()
	id := m.next
	m.next++
	m.watchers[id] = notify
	m.mu.Unlock()
	return func() { m.mu.Lock(); delete(m.watchers, id); m.mu.Unlock() }, nil
}

// Optional suppresses only ErrNotFound. Parse, permission and network errors fail.
func Optional(source Source) Source { return &optional{source} }

type optional struct{ Source }

func (s *optional) Read(ctx context.Context, schema *sp.Schema) (Layer, error) {
	l, err := s.Source.Read(ctx, schema)
	if errors.Is(err, ErrNotFound) {
		return Layer{}, nil
	}
	return l, err
}
func (s *optional) Watch(ctx context.Context, notify func()) (func(), error) {
	if w, ok := s.Source.(Watcher); ok {
		return w.Watch(ctx, notify)
	}
	return func() {}, nil
}

// Poll adds periodic invalidation to a source. It does not cache failed reads.
func Poll(source Source, interval time.Duration) Source { return &polling{source, interval} }

type polling struct {
	Source
	interval time.Duration
}

func (p *polling) Watch(ctx context.Context, notify func()) (func(), error) {
	if p.interval <= 0 {
		return nil, errors.New("poll interval must be positive")
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				notify()
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}
