// Package xconf assembles ordered configuration layers against a schemapb schema.
// Defaults, coercion and validation run once after merging. Reloads publish only
// complete, validated snapshots.
package xconf

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	"google.golang.org/protobuf/proto"
)

// Path addresses an object member or list index. String returns a JSON pointer.
type Path []string

func (p Path) String() string {
	var b strings.Builder
	for _, s := range p {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1"))
	}
	return b.String()
}
func child(p Path, k string) Path { return append(append(Path(nil), p...), k) }

// Location describes a source position, never its value.
type Location struct {
	Name         string
	Line, Column int
}
type Origin struct {
	Source, Revision, Operation string
	Location                    Location
}
type EditKind uint8

const (
	Replace EditKind = iota + 1
	Delete
)

// Edit operates on object members. Lists are replaced as a whole.
type Edit struct {
	Kind  EditKind
	Path  Path
	Value any
}
type Layer struct {
	Values    map[string]any
	Revision  string
	Locations map[string]Location // keys are JSON pointers
	Edits     []Edit              // applied in order after Values
	// Stale reports a failing source: the layer is its last good one (or empty).
	// The raw error is exposed only through Snapshot.Degraded.
	Stale error
}

// Source reads a complete partial layer. Read must honor ctx and return owned
// data (or data it will not mutate). The schema argument is an isolated copy.
// Sources must not apply schema defaults or require a complete configuration.
type Source interface {
	Name() string
	Read(context.Context, *sp.Schema) (Layer, error)
}

// Watcher registers notifications synchronously, before the initial Read.
// Notifications invalidate the whole layer. stop must release resources and
// wait for callbacks to finish. Read and notifications may run concurrently.
type Watcher interface {
	Watch(context.Context, func()) (stop func(), err error)
}

var ErrNotFound = errors.New("xconf: source not found")
var ErrClosed = errors.New("xconf: runtime closed")

// SourceError omits provider error text, which may contain secrets.
// The original error remains available via errors.Is/As/Unwrap.
type SourceError struct {
	Source, Stage string
	Cause         error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("xconf: source %q: %s failed", e.Source, e.Stage)
}
func (e *SourceError) Unwrap() error { return e.Cause }

// ValidationError carries diagnostics without including input values in Error().
type ValidationError struct {
	Result *sp.ValidationResult
	Report *sp.ResolveReport
}

func (e *ValidationError) Error() string {
	var items []string
	for _, v := range e.Result.GetErrors() {
		items = append(items, v.GetPath()+": "+v.GetCode().String())
	}
	return "xconf: validation failed: " + strings.Join(items, "; ")
}

// Snapshot owns immutable state. Accessors returning mutable data clone it.
type Snapshot struct {
	baked      *sp.Baked
	validation *sp.ValidationResult
	report     *sp.ResolveReport
	origins    map[string][]Origin
	revisions  map[string]string
	degraded   map[string]error
	version    uint64
	loadedAt   time.Time
}

func (s *Snapshot) Version() uint64           { return s.version }
func (s *Snapshot) LoadedAt() time.Time       { return s.loadedAt }
func (s *Snapshot) Baked() *sp.Baked          { return proto.Clone(s.baked).(*sp.Baked) }
func (s *Snapshot) Report() *sp.ResolveReport { return proto.Clone(s.report).(*sp.ResolveReport) }
func (s *Snapshot) Validation() *sp.ValidationResult {
	return proto.Clone(s.validation).(*sp.ValidationResult)
}
func (s *Snapshot) Revisions() map[string]string {
	m := map[string]string{}
	for k, v := range s.revisions {
		m[k] = v
	}
	return m
}

// Degraded returns the raw errors of sources serving stale layers, keyed by source
// name. It is empty when every source is healthy. Errors may contain secrets.
func (s *Snapshot) Degraded() map[string]error {
	m := map[string]error{}
	for k, v := range s.degraded {
		m[k] = v
	}
	return m
}

// Explain returns source writes followed by schema operations at this exact path.
// Records contain no values, including overridden values.
func (s *Snapshot) Explain(path ...string) []Origin {
	return append([]Origin(nil), s.origins[Path(path).String()]...)
}

// Origins returns all provenance keyed by JSON pointer, including schema operations.
// The map and each history slice are independent copies. Records contain no values.
func (s *Snapshot) Origins() map[string][]Origin {
	origins := make(map[string][]Origin, len(s.origins))
	for path, history := range s.origins {
		origins[path] = append([]Origin(nil), history...)
	}
	return origins
}

func (s *Snapshot) Decode(target any) error { return s.Baked().Decode(target) }
func Decode[T any](s *Snapshot) (T, error) {
	var value T
	if s == nil {
		return value, errors.New("xconf: nil snapshot")
	}
	err := s.Decode(&value)
	return value, err
}
func reportPath(parts []*sp.PathSegment) Path {
	p := Path{}
	for _, part := range parts {
		switch v := part.Segment.(type) {
		case *sp.PathSegment_Key:
			p = append(p, v.Key)
		case *sp.PathSegment_Index:
			p = append(p, strconv.FormatUint(v.Index, 10))
		}
	}
	return p
}

type loader struct {
	schema  *sp.Schema
	engine  *sp.Engine
	sources []Source
}

// Loader holds an owned schema and compiled engine for repeated loads.
// Compile options (including custom formats and CEL cost limits) pass to schemapb.
type Loader struct{ base *loader }

func New(schema *sp.Schema, options ...sp.CompileOption) (*Loader, error) {
	if schema == nil {
		return nil, errors.New("xconf: nil schema")
	}
	owned := proto.Clone(schema).(*sp.Schema)
	engine, err := sp.Compile(owned, options...)
	if err != nil {
		return nil, err
	}
	return &Loader{base: &loader{schema: owned, engine: engine}}, nil
}
func (l *Loader) bind(sources []Source) (*loader, error) {
	seen := map[string]bool{}
	for _, source := range sources {
		if source == nil || source.Name() == "" {
			return nil, errors.New("xconf: source requires a name")
		}
		if seen[source.Name()] {
			return nil, fmt.Errorf("xconf: duplicate source %q", source.Name())
		}
		seen[source.Name()] = true
	}
	out := *l.base
	out.sources = append([]Source(nil), sources...)
	return &out, nil
}
func newLoader(schema *sp.Schema, sources []Source) (*loader, error) {
	l, err := New(schema)
	if err != nil {
		return nil, err
	}
	return l.bind(sources)
}
func (l *Loader) Load(ctx context.Context, sources ...Source) (*Snapshot, error) {
	bound, err := l.bind(sources)
	if err != nil {
		return nil, err
	}
	s, err := bound.load(ctx)
	if err == nil {
		s.version = 1
	}
	return s, err
}

func Load(ctx context.Context, schema *sp.Schema, sources ...Source) (*Snapshot, error) {
	l, err := newLoader(schema, sources)
	if err != nil {
		return nil, err
	}
	s, err := l.load(ctx)
	if err == nil {
		s.version = 1
	}
	return s, err
}
func LoadAs[T any](ctx context.Context, schema *sp.Schema, sources ...Source) (T, error) {
	s, err := Load(ctx, schema, sources...)
	if err != nil {
		var zero T
		return zero, err
	}
	return Decode[T](s)
}
func (l *loader) load(ctx context.Context) (*Snapshot, error) {
	state := &merger{values: map[string]any{}, origins: map[string][]Origin{}, schema: l.schema}
	revisions, degraded := map[string]string{}, map[string]error{}
	for _, source := range l.sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		layer, err := source.Read(ctx, proto.Clone(l.schema).(*sp.Schema))
		if err != nil {
			return nil, &SourceError{source.Name(), "read", err}
		}
		if err = state.apply(source.Name(), layer); err != nil {
			return nil, &SourceError{source.Name(), "merge", err}
		}
		revisions[source.Name()] = layer.Revision
		if layer.Stale != nil {
			degraded[source.Name()] = layer.Stale
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	baked, result, report, err := l.engine.BakeDetailed(state.values)
	if err != nil {
		return nil, fmt.Errorf("xconf: bake failed: %w", err)
	}
	if result.Blocking() {
		return nil, &ValidationError{result, report}
	}
	for _, event := range report.GetEvents() {
		key := reportPath(event.GetPathSegments()).String()
		state.origins[key] = append(state.origins[key], Origin{Source: "schema", Operation: event.GetOperation().String()})
	}
	return &Snapshot{baked: baked, validation: result, report: report, origins: state.origins, revisions: revisions, degraded: degraded, loadedAt: time.Now()}, nil
}
