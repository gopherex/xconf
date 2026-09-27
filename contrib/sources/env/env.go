// Package env maps a captured environment to schema paths. It never modifies os.Environ.
package env

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"unicode"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
)

type Option func(*Source)
type Source struct {
	id, prefix  string
	environment []string
	injected    bool
	strict      bool
	bindings    map[string]x.Path
	splits      map[string]string
}

func New(opts ...Option) *Source {
	s := &Source{id: "env", bindings: map[string]x.Path{}, splits: map[string]string{}}
	for _, o := range opts {
		o(s)
	}
	return s
}
func Name(id string) Option       { return func(s *Source) { s.id = id } }
func Prefix(prefix string) Option { return func(s *Source) { s.prefix = prefix } }
func Strict() Option              { return func(s *Source) { s.strict = true } }

// Environment captures a supplied environment; even an empty slice disables OS reads.
func Environment(values []string) Option {
	return func(s *Source) { s.environment = append([]string(nil), values...); s.injected = true }
}

// Bind uses an exact environment name, without adding Prefix.
func Bind(name string, path ...string) Option {
	return func(s *Source) { s.bindings[name] = append(x.Path(nil), path...) }
}

// Split overrides JSON collection syntax with a literal separator for this name.
// An explicitly empty value produces an empty list.
func Split(name, separator string) Option { return func(s *Source) { s.splits[name] = separator } }
func (s *Source) Name() string            { return s.id }
func (s *Source) Read(ctx context.Context, schema *sp.Schema) (x.Layer, error) {
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	values := s.environment
	if !s.injected {
		values = os.Environ()
	}
	vars := map[string]string{}
	for _, entry := range values {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			vars[k] = v
		}
	}
	return s.decode(schema, vars)
}

// Decode maps already parsed environment variables, for adapters such as dotenv.
func Decode(schema *sp.Schema, vars map[string]string, opts ...Option) (x.Layer, error) {
	return New(opts...).decode(schema, vars)
}

type binding struct {
	path      x.Path
	container bool
	ambiguous bool
}

func snake(s string) string {
	r := []rune(s)
	var out []rune
	for i, c := range r {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			out = append(out, '_')
			continue
		}
		if unicode.IsUpper(c) && i > 0 && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]) || (i+1 < len(r) && unicode.IsLower(r[i+1]) && unicode.IsUpper(r[i-1]))) {
			out = append(out, '_')
		}
		out = append(out, unicode.ToUpper(c))
	}
	return string(out)
}
func (s *Source) decode(schema *sp.Schema, vars map[string]string) (x.Layer, error) {
	if schema == nil {
		return x.Layer{}, errors.New("nil schema")
	}
	bindings := map[string]binding{}
	var visit func(*sp.Schema, x.Path, map[*sp.Schema]bool) error
	visit = func(sc *sp.Schema, path x.Path, stack map[*sp.Schema]bool) error {
		if stack[sc] {
			return nil
		}
		stack[sc] = true
		defer delete(stack, sc)
		for _, f := range sc.GetFields() {
			if f.GetComputed() != nil {
				continue
			}
			p := append(append(x.Path(nil), path...), f.GetName())
			parts := make([]string, len(p))
			for i, k := range p {
				parts[i] = snake(k)
			}
			name := s.prefix + strings.Join(parts, "_")
			b := binding{p, f.GetObject() != nil || f.GetRef() != nil || f.GetMap() != nil || f.GetList() != nil || f.GetOneOf() != nil || f.GetJson() != nil, false}
			if old, exists := bindings[name]; exists {
				if !reflect.DeepEqual(old.path, p) {
					return fmt.Errorf("ambiguous environment name %q", name)
				}
				b.ambiguous = old.ambiguous || old.container != b.container
			}
			bindings[name] = b
			sub := f.GetObject().GetSchema()
			if ref := f.GetRef(); ref != nil {
				key := ref.GetName()
				if id := ref.GetId(); id != nil {
					key = id.GetNamespace() + "\x00" + id.GetName() + "\x00" + id.GetVersion()
				}
				sub = schema.GetDefs()[key]
			}
			if sub != nil {
				if err := visit(sub, p, stack); err != nil {
					return err
				}
			}
			if union := f.GetOneOf(); union != nil {
				keys := make([]string, 0, len(union.Variants))
				for key := range union.Variants {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if err := visit(union.Variants[key], p, stack); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := visit(schema, nil, map[*sp.Schema]bool{}); err != nil {
		return x.Layer{}, err
	}
	for name, path := range s.bindings {
		if len(path) == 0 {
			return x.Layer{}, errors.New("binding requires a path")
		}
		b := binding{path: path}
		for _, candidate := range bindings {
			if reflect.DeepEqual(candidate.path, path) {
				b.container = candidate.container
				b.ambiguous = candidate.ambiguous
				break
			}
		}
		bindings[name] = b
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	layer := x.Layer{Values: map[string]any{}, Locations: map[string]x.Location{}}
	assigned := map[string]bool{}
	for _, name := range keys {
		b, ok := bindings[name]
		if !ok {
			if s.strict && s.prefix != "" && strings.HasPrefix(name, s.prefix) {
				return x.Layer{}, fmt.Errorf("unknown environment name %q", name)
			}
			continue
		}
		if b.ambiguous {
			return x.Layer{}, fmt.Errorf("ambiguous container encoding for %s; supply the whole variant as JSON", name)
		}
		raw := vars[name]
		var value any = raw
		if sep, split := s.splits[name]; split {
			if sep == "" {
				return x.Layer{}, errors.New("split separator cannot be empty")
			}
			list := []any{}
			if raw != "" {
				for _, v := range strings.Split(raw, sep) {
					list = append(list, v)
				}
			}
			value = list
		} else if b.container {
			d := json.NewDecoder(bytes.NewBufferString(raw))
			d.UseNumber()
			if err := d.Decode(&value); err != nil {
				return x.Layer{}, fmt.Errorf("invalid JSON in %s", name)
			}
			var extra any
			if err := d.Decode(&extra); err != io.EOF {
				return x.Layer{}, fmt.Errorf("trailing JSON in %s", name)
			}
		}
		key := b.path.String()
		for p := range assigned {
			if p == key || strings.HasPrefix(p, key+"/") || strings.HasPrefix(key, p+"/") {
				return x.Layer{}, fmt.Errorf("overlapping environment bindings at %s", key)
			}
		}
		assigned[key] = true
		dst := layer.Values
		for _, part := range b.path[:len(b.path)-1] {
			next, exists := dst[part]
			if !exists {
				next = map[string]any{}
				dst[part] = next
			}
			m, ok := next.(map[string]any)
			if !ok {
				return x.Layer{}, errors.New("binding parent is not an object")
			}
			dst = m
		}
		dst[b.path[len(b.path)-1]] = value
		layer.Locations[key] = x.Location{Name: name}
	}
	return layer, nil
}
