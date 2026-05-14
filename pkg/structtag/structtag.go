// Package structtag provides a struct-first entry point to xconf.
//
// Instead of declaring a fluent Schema, callers can annotate an existing
// Go struct with `xconf:"..."` tags and let SchemaFromStruct synthesize the
// schema at runtime.
//
//	type Config struct {
//	    Port int    `xconf:"default=8080"`
//	    DSN  string `xconf:"env=DB_DSN,required"`
//	    Tags []string `xconf:"split=,"`
//	}
//
//	schema, err := structtag.SchemaFromStruct[Config]("AppConfig")
//	cfg, err := load.Load(schema, &Config{}, load.FromEnv(nil))
//
// Supported tag keys:
//
//	env=NAME      explicit env name (overrides auto-derive)
//	default=V     default value (string-parsed against the field's Go type)
//	required      flag, no value
//	desc=...      description (commas not supported in value)
//	split=SEP     entry separator for slice/map env decoding
//	kv=SEP        key/value separator for map env decoding
//	skip          flag, ignore this field
//
// Validators are NOT supported via tags (they require typed closures).
// Compose with the fluent API for validator attachment.
package structtag

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gopherex/xconf"
)

// SchemaFromStruct builds a schema from the type parameter T.
func SchemaFromStruct[T any](rootName string) (*xconf.Schema, error) {
	var zero T
	rt := reflect.TypeOf(zero)
	if rt == nil {
		return nil, fmt.Errorf("structtag: nil type")
	}
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil, fmt.Errorf("structtag: T must be a struct, got %v", rt.Kind())
	}
	if rootName == "" {
		rootName = rt.Name()
	}
	nodes, err := nodesFromStruct(rt)
	if err != nil {
		return nil, err
	}
	return xconf.Define(rootName, nodes...), nil
}

func nodesFromStruct(rt reflect.Type) ([]xconf.Node, error) {
	var nodes []xconf.Node
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		opts := parseTag(sf.Tag.Get("xconf"))
		if opts.skip {
			continue
		}
		node, err := nodeFromField(sf, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sf.Name, err)
		}
		if node != nil {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func nodeFromField(sf reflect.StructField, opts tagOpts) (xconf.Node, error) {
	ft := sf.Type
	if ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}

	// Nested struct → Group.
	if ft.Kind() == reflect.Struct && !isSpecialStruct(ft) {
		children, err := nodesFromStruct(ft)
		if err != nil {
			return nil, err
		}
		return xconf.Group(sf.Name, children...), nil
	}

	switch ft.Kind() {
	case reflect.Slice:
		return sliceNode(sf, ft, opts)
	case reflect.Map:
		return mapNode(sf, ft, opts)
	default:
		return scalarNode(sf, ft, opts)
	}
}

func scalarNode(sf reflect.StructField, ft reflect.Type, opts tagOpts) (xconf.Node, error) {
	name := sf.Name
	if ft.PkgPath() == "time" {
		switch ft.Name() {
		case "Duration":
			return apply(xconf.Duration(name), opts)
		case "Time":
			return apply(xconf.Time(name), opts)
		}
	}
	switch ft.Kind() {
	case reflect.Int:
		return apply(xconf.Int(name), opts)
	case reflect.Int8:
		return apply(xconf.Int8(name), opts)
	case reflect.Int16:
		return apply(xconf.Int16(name), opts)
	case reflect.Int32:
		return apply(xconf.Int32(name), opts)
	case reflect.Int64:
		return apply(xconf.Int64(name), opts)
	case reflect.Uint:
		return apply(xconf.Uint(name), opts)
	case reflect.Uint8:
		return apply(xconf.Uint8(name), opts)
	case reflect.Uint16:
		return apply(xconf.Uint16(name), opts)
	case reflect.Uint32:
		return apply(xconf.Uint32(name), opts)
	case reflect.Uint64:
		return apply(xconf.Uint64(name), opts)
	case reflect.Float32:
		return apply(xconf.Float32(name), opts)
	case reflect.Float64:
		return apply(xconf.Float64(name), opts)
	case reflect.String:
		return apply(xconf.String(name), opts)
	case reflect.Bool:
		return apply(xconf.Bool(name), opts)
	}
	return nil, fmt.Errorf("unsupported scalar type %v", ft)
}

// apply uses a small interface satisfied by *Field[T] so option application
// works generically. We use reflection here because Field[T] is generic and
// we cannot name it without T.
func apply(field any, opts tagOpts) (xconf.Node, error) {
	rv := reflect.ValueOf(field)
	if opts.env != "" {
		callChain(rv, "Env", reflect.ValueOf(opts.env))
	}
	if opts.required {
		callChain(rv, "Required")
	}
	if opts.desc != "" {
		callChain(rv, "Description", reflect.ValueOf(opts.desc))
	}
	if opts.defaultVal != "" {
		if err := applyDefault(rv, opts.defaultVal); err != nil {
			return nil, err
		}
	}
	return field.(xconf.Node), nil
}

func callChain(rv reflect.Value, method string, args ...reflect.Value) {
	m := rv.MethodByName(method)
	if !m.IsValid() {
		return
	}
	m.Call(args)
}

func applyDefault(rv reflect.Value, s string) error {
	// Field[T].Default takes T. Determine T via the method signature.
	m := rv.MethodByName("Default")
	if !m.IsValid() {
		return fmt.Errorf("no Default method")
	}
	argT := m.Type().In(0)
	parsed, err := parseScalar(argT, s)
	if err != nil {
		return fmt.Errorf("default %q: %w", s, err)
	}
	m.Call([]reflect.Value{parsed})
	return nil
}

func parseScalar(t reflect.Type, s string) (reflect.Value, error) {
	if t.PkgPath() == "time" {
		switch t.Name() {
		case "Duration":
			d, err := time.ParseDuration(s)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(d), nil
		case "Time":
			tm, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(tm), nil
		}
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(n).Convert(t), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(n).Convert(t), nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(f).Convert(t), nil
	case reflect.String:
		return reflect.ValueOf(s), nil
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(b), nil
	}
	return reflect.Value{}, fmt.Errorf("unsupported default type %v", t)
}

func sliceNode(sf reflect.StructField, ft reflect.Type, opts tagOpts) (xconf.Node, error) {
	elemT := ft.Elem()
	field, err := makeSliceField(sf.Name, elemT)
	if err != nil {
		return nil, err
	}
	rv := reflect.ValueOf(field)
	if opts.env != "" {
		callChain(rv, "Env", reflect.ValueOf(opts.env))
	}
	if opts.required {
		callChain(rv, "Required")
	}
	if opts.desc != "" {
		callChain(rv, "Description", reflect.ValueOf(opts.desc))
	}
	if opts.split != "" {
		callChain(rv, "EnvSplit", reflect.ValueOf(opts.split))
	}
	return field.(xconf.Node), nil
}

func mapNode(sf reflect.StructField, ft reflect.Type, opts tagOpts) (xconf.Node, error) {
	keyT := ft.Key()
	valT := ft.Elem()
	field, err := makeMapField(sf.Name, keyT, valT)
	if err != nil {
		return nil, err
	}
	rv := reflect.ValueOf(field)
	if opts.env != "" {
		callChain(rv, "Env", reflect.ValueOf(opts.env))
	}
	if opts.required {
		callChain(rv, "Required")
	}
	if opts.desc != "" {
		callChain(rv, "Description", reflect.ValueOf(opts.desc))
	}
	if opts.split != "" {
		callChain(rv, "EnvSplit", reflect.ValueOf(opts.split))
	}
	if opts.kv != "" {
		callChain(rv, "KVSplit", reflect.ValueOf(opts.kv))
	}
	return field.(xconf.Node), nil
}

// makeSliceField calls xconf.Slice[T] with T derived from elemT via a small
// switch. Generic instantiation can't be done via reflection alone, so we
// enumerate supported element types.
func makeSliceField(name string, elemT reflect.Type) (any, error) {
	if elemT.PkgPath() == "time" {
		switch elemT.Name() {
		case "Duration":
			return xconf.Slice[time.Duration](name), nil
		case "Time":
			return xconf.Slice[time.Time](name), nil
		}
	}
	switch elemT.Kind() {
	case reflect.Int:
		return xconf.Slice[int](name), nil
	case reflect.Int64:
		return xconf.Slice[int64](name), nil
	case reflect.Float64:
		return xconf.Slice[float64](name), nil
	case reflect.String:
		return xconf.Slice[string](name), nil
	case reflect.Bool:
		return xconf.Slice[bool](name), nil
	}
	return nil, fmt.Errorf("unsupported slice element type %v", elemT)
}

func makeMapField(name string, keyT, valT reflect.Type) (any, error) {
	// Only a representative set; extend as needed.
	if keyT.Kind() == reflect.String {
		switch valT.Kind() {
		case reflect.Int:
			return xconf.Map[string, int](name), nil
		case reflect.Int64:
			return xconf.Map[string, int64](name), nil
		case reflect.Float64:
			return xconf.Map[string, float64](name), nil
		case reflect.String:
			return xconf.Map[string, string](name), nil
		case reflect.Bool:
			return xconf.Map[string, bool](name), nil
		}
	}
	return nil, fmt.Errorf("unsupported map type map[%v]%v", keyT, valT)
}

func isSpecialStruct(t reflect.Type) bool {
	return t.PkgPath() == "time" && (t.Name() == "Time" || t.Name() == "Duration")
}

// ---------------------------------------------------------------------------
// Tag parsing
// ---------------------------------------------------------------------------

type tagOpts struct {
	env        string
	defaultVal string
	required   bool
	desc       string
	split      string
	kv         string
	skip       bool
}

func parseTag(raw string) tagOpts {
	var o tagOpts
	if raw == "" {
		return o
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, hasV := strings.Cut(part, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "env":
			if hasV {
				o.env = v
			}
		case "default":
			if hasV {
				o.defaultVal = v
			}
		case "required":
			o.required = true
		case "desc":
			if hasV {
				o.desc = v
			}
		case "split":
			if hasV {
				o.split = v
			}
		case "kv":
			if hasV {
				o.kv = v
			}
		case "skip", "-":
			o.skip = true
		}
	}
	return o
}
