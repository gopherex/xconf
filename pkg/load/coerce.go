package load

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gopherex/xconf"
)

// setField coerces raw into the appropriate type for d.Kind and assigns it
// to fv via reflection.
func setField(d xconf.FieldDesc, raw any, fv reflect.Value) error {
	switch d.Kind {
	case xconf.KindSlice:
		return setSlice(d, raw, fv)
	case xconf.KindMap:
		return setMap(d, raw, fv)
	default:
		return setScalar(d.Kind, raw, fv)
	}
}

func setScalar(kind xconf.Kind, raw any, fv reflect.Value) error {
	switch kind {
	case xconf.KindInt, xconf.KindInt8, xconf.KindInt16, xconf.KindInt32, xconf.KindInt64:
		n, err := toInt64(raw)
		if err != nil {
			return err
		}
		if fv.OverflowInt(n) {
			return fmt.Errorf("int %d overflows %v", n, fv.Type())
		}
		fv.SetInt(n)
	case xconf.KindUint, xconf.KindUint8, xconf.KindUint16, xconf.KindUint32, xconf.KindUint64:
		n, err := toUint64(raw)
		if err != nil {
			return err
		}
		if fv.OverflowUint(n) {
			return fmt.Errorf("uint %d overflows %v", n, fv.Type())
		}
		fv.SetUint(n)
	case xconf.KindFloat32, xconf.KindFloat64:
		f, err := toFloat64(raw)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	case xconf.KindString:
		s, err := toString(raw)
		if err != nil {
			return err
		}
		fv.SetString(s)
	case xconf.KindBytes:
		s, err := toString(raw)
		if err != nil {
			return err
		}
		fv.SetBytes([]byte(s))
	case xconf.KindBool:
		b, err := toBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case xconf.KindDuration:
		d, err := toDuration(raw)
		if err != nil {
			return err
		}
		fv.SetInt(int64(d))
	case xconf.KindTime:
		t, err := toTime(raw)
		if err != nil {
			return err
		}
		fv.Set(reflect.ValueOf(t))
	default:
		return fmt.Errorf("unsupported scalar kind %v", kind)
	}
	return nil
}

func setSlice(d xconf.FieldDesc, raw any, fv reflect.Value) error {
	items, err := toItems(raw, d.EnvSplit)
	if err != nil {
		return err
	}
	elemType := fv.Type().Elem()
	out := reflect.MakeSlice(fv.Type(), len(items), len(items))
	for i, it := range items {
		ev := reflect.New(elemType).Elem()
		if err := setScalar(d.ElemKind, it, ev); err != nil {
			return fmt.Errorf("[%d]: %w", i, err)
		}
		out.Index(i).Set(ev)
	}
	fv.Set(out)
	return nil
}

func setMap(d xconf.FieldDesc, raw any, fv reflect.Value) error {
	entries, err := toEntries(raw, d.EnvSplit, d.KVSplit)
	if err != nil {
		return err
	}
	keyType := fv.Type().Key()
	valType := fv.Type().Elem()
	out := reflect.MakeMapWithSize(fv.Type(), len(entries))
	for k, v := range entries {
		kv := reflect.New(keyType).Elem()
		if err := setScalar(d.KeyKind, k, kv); err != nil {
			return fmt.Errorf("key %v: %w", k, err)
		}
		vv := reflect.New(valType).Elem()
		if err := setScalar(d.ElemKind, v, vv); err != nil {
			return fmt.Errorf("value for key %v: %w", k, err)
		}
		out.SetMapIndex(kv, vv)
	}
	fv.Set(out)
	return nil
}

// ---------------------------------------------------------------------------
// Scalar coercers
// ---------------------------------------------------------------------------

func toInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	}
	return 0, fmt.Errorf("cannot coerce %T to int", raw)
}

func toUint64(raw any) (uint64, error) {
	switch v := raw.(type) {
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case int, int8, int16, int32, int64:
		n, _ := toInt64(raw)
		if n < 0 {
			return 0, fmt.Errorf("negative value %d for uint", n)
		}
		return uint64(n), nil
	case float64:
		if v < 0 {
			return 0, fmt.Errorf("negative value %v for uint", v)
		}
		return uint64(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, err
		}
		if n < 0 {
			return 0, fmt.Errorf("negative value %d for uint", n)
		}
		return uint64(n), nil
	case string:
		return strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	}
	return 0, fmt.Errorf("cannot coerce %T to uint", raw)
}

func toFloat64(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int, int8, int16, int32, int64:
		n, _ := toInt64(raw)
		return float64(n), nil
	case uint, uint8, uint16, uint32, uint64:
		n, _ := toUint64(raw)
		return float64(n), nil
	case json.Number:
		return v.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	}
	return 0, fmt.Errorf("cannot coerce %T to float", raw)
}

func toString(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case fmt.Stringer:
		return v.String(), nil
	}
	return fmt.Sprintf("%v", raw), nil
}

func toBool(raw any) (bool, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(strings.TrimSpace(v))
	case int:
		return v != 0, nil
	case float64:
		return v != 0, nil
	}
	return false, fmt.Errorf("cannot coerce %T to bool", raw)
}

func toDuration(raw any) (time.Duration, error) {
	switch v := raw.(type) {
	case time.Duration:
		return v, nil
	case string:
		return time.ParseDuration(strings.TrimSpace(v))
	case int, int8, int16, int32, int64:
		n, _ := toInt64(raw)
		return time.Duration(n), nil
	case float64:
		return time.Duration(v), nil
	}
	return 0, fmt.Errorf("cannot coerce %T to duration", raw)
}

func toTime(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		return time.Parse(time.RFC3339, strings.TrimSpace(v))
	}
	return time.Time{}, fmt.Errorf("cannot coerce %T to time", raw)
}

// ---------------------------------------------------------------------------
// Collection adapters
// ---------------------------------------------------------------------------

func toItems(raw any, envSplit string) ([]any, error) {
	switch v := raw.(type) {
	case []any:
		return v, nil
	case string:
		sep := envSplit
		if sep == "" {
			sep = ","
		}
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, nil
		}
		parts := strings.Split(v, sep)
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = strings.TrimSpace(p)
		}
		return out, nil
	}
	// reflect-based fallback for typed slices (e.g. []string from a default).
	rv := reflect.ValueOf(raw)
	if rv.Kind() == reflect.Slice {
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out, nil
	}
	return nil, fmt.Errorf("cannot coerce %T to slice", raw)
}

func toEntries(raw any, envSplit, kvSplit string) (map[any]any, error) {
	switch v := raw.(type) {
	case map[string]any:
		out := make(map[any]any, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out, nil
	case string:
		entrySep := envSplit
		if entrySep == "" {
			entrySep = ","
		}
		kvSep := kvSplit
		if kvSep == "" {
			kvSep = "="
		}
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, nil
		}
		out := map[any]any{}
		for _, part := range strings.Split(v, entrySep) {
			k, val, ok := strings.Cut(strings.TrimSpace(part), kvSep)
			if !ok {
				return nil, fmt.Errorf("malformed map entry %q", part)
			}
			out[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
		return out, nil
	}
	rv := reflect.ValueOf(raw)
	if rv.Kind() == reflect.Map {
		out := make(map[any]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().Interface()] = iter.Value().Interface()
		}
		return out, nil
	}
	return nil, fmt.Errorf("cannot coerce %T to map", raw)
}
