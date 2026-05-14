package core

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"unicode"
)

// Schema represents a named group of configuration nodes. It is also the
// public composition unit: external libraries export a *Schema that consumers
// can Embed under any name.
type Schema struct {
	name       string
	children   []Node
	bindType   string
	bindLoader string
	loaderFn   func(any) error

	// envPrefix is the explicit prefix override. nil means "derive from name"
	// (or "no prefix" for the root). A non-nil pointer to "" disables prefixing.
	envPrefix *string
	isRoot    bool
}

// Define is the top-level constructor used by application schemas. By default
// it contributes no env prefix; child fields get env names derived directly
// from their own names.
func Define(name string, fields ...Node) *Schema {
	empty := ""
	return &Schema{name: name, children: fields, envPrefix: &empty, isRoot: true}
}

// Group is a nested schema. Codegen emits a nested struct type for it. By
// default child env names are prefixed with the group's name converted to
// SCREAMING_SNAKE_CASE.
func Group(name string, fields ...Node) *Schema {
	return &Schema{name: name, children: fields}
}

// EnvPrefix overrides the env-name prefix contributed by this schema. Pass ""
// to disable prefixing for this group's children.
func (s *Schema) EnvPrefix(prefix string) *Schema {
	s.envPrefix = &prefix
	return s
}

// GroupAs is a nested schema bound to an existing Go type T. The type is
// captured at compile time via the generic parameter; codegen reuses T
// directly instead of emitting a fresh struct.
//
//	var ConfigSchema = xconf.GroupAs[Config]("Config",
//	    xconf.String("Addr").Env("REDIS_ADDR"),
//	)
//
// T must be a named, exported struct type from a non-empty package path.
func GroupAs[T any](name string, fields ...Node) *Schema {
	t := reflect.TypeFor[T]()
	if t == nil || t.Name() == "" || t.PkgPath() == "" {
		panic(fmt.Sprintf("xconf.GroupAs: type parameter must be a named type, got %v", t))
	}
	return &Schema{
		name:     name,
		children: fields,
		bindType: t.PkgPath() + "." + t.Name(),
	}
}

// WithLoader binds a custom loader function to a schema. The function's
// fully-qualified name is captured via runtime reflection; codegen delegates
// loading to it instead of inlining env-loading.
//
//	var ConfigSchema = xconf.WithLoader(
//	    xconf.GroupAs[Config]("Config", ...),
//	    LoadConfig,
//	)
func WithLoader[T any](s *Schema, loader func() (*T, error)) *Schema {
	pc := reflect.ValueOf(loader).Pointer()
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		panic("xconf.WithLoader: cannot resolve loader function name")
	}
	s.bindLoader = fn.Name()
	// Adapter for the reflection-based runtime loader.
	// target is *reflect.Value pointing at the field's struct slot.
	s.loaderFn = func(target any) error {
		fv, ok := target.(reflect.Value)
		if !ok {
			return fmt.Errorf("xconf: loader target must be reflect.Value, got %T", target)
		}
		v, err := loader()
		if err != nil {
			return err
		}
		if v == nil {
			return fmt.Errorf("xconf: bound loader %s returned nil", s.bindLoader)
		}
		fv.Set(reflect.ValueOf(*v))
		return nil
	}
	return s
}

// Embed re-roots an external schema under the given name, preserving all
// child fields, bindings, and validators. The embedded schema's env prefix
// is recomputed from the new name (use EnvPrefix to override).
//
//	var Schema = xconf.Define("App",
//	    xconf.Int("Port"),              // env: PORT
//	    xconf.Embed("Redis", redislib.ConfigSchema), // envs: REDIS_*
//	)
func Embed(name string, sub *Schema) *Schema {
	cp := *sub
	cp.name = name
	cp.envPrefix = nil // re-derive from new name
	cp.isRoot = false
	return &cp
}

// Describe returns the serializable tree for this schema with auto-derived
// env names resolved.
func (s *Schema) Describe() FieldDesc { return s.describeWithPrefix("") }

func (s *Schema) describeWithPrefix(parentPrefix string) FieldDesc {
	prefix := s.resolvePrefix(parentPrefix)
	d := FieldDesc{
		Name:       s.name,
		Kind:       KindGroup,
		GoType:     s.name,
		BindType:   s.bindType,
		BindLoader: s.bindLoader,
		LoaderFn:   s.loaderFn,
	}
	for _, c := range s.children {
		var cd FieldDesc
		if sub, ok := c.(*Schema); ok {
			cd = sub.describeWithPrefix(prefix)
		} else {
			cd = c.describe()
			if cd.Env == "" {
				cd.Env = joinEnv(prefix, toScreamingSnake(cd.Name))
			}
		}
		d.Children = append(d.Children, cd)
	}
	return d
}

// describe satisfies Node so Schema can be used as a child of another Schema.
// The auto-env walk uses describeWithPrefix; this fallback is only used if a
// schema is described in isolation.
func (s *Schema) describe() FieldDesc { return s.describeWithPrefix("") }

func (s *Schema) resolvePrefix(parent string) string {
	var own string
	switch {
	case s.envPrefix != nil:
		own = *s.envPrefix
	case s.isRoot:
		own = ""
	default:
		own = toScreamingSnake(s.name)
	}
	return joinEnv(parent, own)
}

func joinEnv(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "_" + b
	}
}

// toScreamingSnake converts CamelCase / camelCase to SCREAMING_SNAKE_CASE.
// Handles consecutive uppercase runs (e.g. "HTTPServer" -> "HTTP_SERVER").
func toScreamingSnake(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			next := rune(0)
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			// boundary: lower->Upper, or Upper->Upper followed by lower
			if unicode.IsLower(prev) || unicode.IsDigit(prev) ||
				(unicode.IsUpper(prev) && next != 0 && unicode.IsLower(next)) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}
