package xconf

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
)

// cloneInput also normalizes Go map/slice aliases into schemapb's native tree.
// A depth bound rejects cycles without walking indefinitely.
func cloneInput(v any, depth int) (any, error) {
	if depth > 128 {
		return nil, fmt.Errorf("input exceeds maximum nesting")
	}
	if v == nil {
		return nil, nil
	}
	switch x := v.(type) {
	case time.Time, time.Duration, json.Number:
		return x, nil
	case []byte:
		return slices.Clone(x), nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map keys must be strings")
		}
		if rv.IsNil() {
			return nil, nil
		}
		out := map[string]any{}
		iter := rv.MapRange()
		for iter.Next() {
			c, err := cloneInput(iter.Value().Interface(), depth+1)
			if err != nil {
				return nil, err
			}
			out[iter.Key().String()] = c
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil, nil
		}
		out := make([]any, rv.Len())
		for i := range out {
			c, err := cloneInput(rv.Index(i).Interface(), depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case reflect.String:
		return rv.String(), nil
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	default:
		return nil, fmt.Errorf("unsupported input kind %s", rv.Kind())
	}
}

type merger struct {
	values  map[string]any
	origins map[string][]Origin
	schema  *sp.Schema
}

func (m *merger) apply(name string, layer Layer) error {
	origin := Origin{Source: name, Revision: layer.Revision, Operation: "set"}
	if layer.Values != nil {
		v, err := cloneInput(layer.Values, 0)
		if err != nil {
			return err
		}
		m.mergeMap(m.values, v.(map[string]any), nil, m.schema, nil, origin, layer.Locations)
	}
	for _, edit := range layer.Edits {
		if edit.Kind != Replace && edit.Kind != Delete {
			return fmt.Errorf("unknown edit kind")
		}
		if len(edit.Path) == 0 {
			return fmt.Errorf("edits require a non-root object path")
		}
		dst := m.values
		for _, key := range edit.Path[:len(edit.Path)-1] {
			v, exists := dst[key]
			if !exists {
				if edit.Kind == Delete {
					dst = nil
					break
				}
				next := map[string]any{}
				dst[key] = next
				dst = next
				continue
			}
			next, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("edit parent is not an object")
			}
			dst = next
		}
		origin.Operation = "delete"
		if edit.Kind == Replace {
			origin.Operation = "replace"
		}
		m.invalidate(edit.Path, origin, layer.Locations)
		if edit.Kind == Delete {
			m.add(edit.Path, origin, layer.Locations)
		}
		if dst == nil {
			continue
		}
		key := edit.Path[len(edit.Path)-1]
		if edit.Kind == Delete {
			delete(dst, key)
			continue
		}
		value, err := cloneInput(edit.Value, 0)
		if err != nil {
			return err
		}
		dst[key] = value
		m.recordTree(value, edit.Path, origin, layer.Locations)
	}
	return nil
}
func (m *merger) add(path Path, o Origin, locations map[string]Location) {
	for i := len(path); i >= 0; i-- {
		if loc, ok := locations[path[:i].String()]; ok {
			o.Location = loc
			break
		}
	}
	m.origins[path.String()] = append(m.origins[path.String()], o)
}
func (m *merger) invalidate(path Path, o Origin, locations map[string]Location) {
	prefix := path.String() + "/"
	for key := range m.origins {
		if strings.HasPrefix(key, prefix) {
			m.origins[key] = append(m.origins[key], o)
		}
	}
}
func (m *merger) recordTree(v any, path Path, o Origin, loc map[string]Location) {
	m.add(path, o, loc)
	switch x := v.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(x)) {
			m.recordTree(x[key], child(path, key), o, loc)
		}
	case []any:
		for i, value := range x {
			m.recordTree(value, child(path, strconv.Itoa(i)), o, loc)
		}
	}
}
func findField(s *sp.Schema, name string) *sp.Schema_Field {
	for _, f := range s.GetFields() {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}
func refSchema(root *sp.Schema, f *sp.Schema_Field) *sp.Schema {
	ref := f.GetRef()
	if ref == nil {
		return nil
	}
	if id := ref.GetId(); id != nil {
		return root.GetDefs()[id.GetNamespace()+"\x00"+id.GetName()+"\x00"+id.GetVersion()]
	}
	return root.GetDefs()[ref.GetName()]
}
func objectSchema(root *sp.Schema, f *sp.Schema_Field, value map[string]any) *sp.Schema {
	if f == nil {
		return nil
	}
	if o := f.GetObject(); o != nil {
		return o.GetSchema()
	}
	if f.GetRef() != nil {
		return refSchema(root, f)
	}
	if o := f.GetOneOf(); o != nil {
		key, _ := value[o.GetDiscriminator()].(string)
		return o.GetVariants()[key]
	}
	return nil
}
func (m *merger) mergeMap(dst, src map[string]any, path Path, schema *sp.Schema, container *sp.Schema_Field, origin Origin, loc map[string]Location) {
	for _, key := range slices.Sorted(maps.Keys(src)) {
		incoming := src[key]
		p := child(path, key)
		field := findField(schema, key)
		if mp := container.GetMap(); mp != nil {
			field = mp.GetValueField()
			if field == nil && mp.GetValueSchema() != nil {
				field = &sp.Schema_Field{Kind: &sp.Schema_Field_Object_{Object: &sp.Schema_Field_Object{Schema: mp.GetValueSchema()}}}
			}
		}
		lower, lok := dst[key].(map[string]any)
		upper, uok := incoming.(map[string]any)
		replace := !lok || !uok
		if union := field.GetOneOf(); union != nil && lok && uok {
			disc := union.GetDiscriminator()
			if next, present := upper[disc]; present && !reflect.DeepEqual(lower[disc], next) {
				replace = true
			}
		}
		if !replace {
			m.add(p, origin, loc)
			shape := objectSchema(m.schema, field, lower)
			m.mergeMap(lower, upper, p, shape, field, origin, loc)
		} else {
			o := origin
			o.Operation = "replaced"
			if _, existed := dst[key]; existed {
				m.invalidate(p, o, loc)
			}
			dst[key] = incoming
			m.recordTree(incoming, p, origin, loc)
		}
	}
}
