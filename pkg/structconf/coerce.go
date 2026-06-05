package structconf

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	durationType = reflect.TypeOf(time.Duration(0))
	timeType     = reflect.TypeOf(time.Time{})
)

// setValue coerces raw into fv. raw is either a string (env var or default tag)
// or an already-typed value coming from the YAML file.
func setValue(fv reflect.Value, raw any) error {
	t := fv.Type()

	switch t {
	case durationType:
		d, err := toDuration(raw)
		if err != nil {
			return err
		}
		fv.SetInt(int64(d))
		return nil
	case timeType:
		tm, err := toTime(raw)
		if err != nil {
			return err
		}
		fv.Set(reflect.ValueOf(tm))
		return nil
	}

	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := toInt64(raw)
		if err != nil {
			return err
		}
		if fv.OverflowInt(n) {
			return fmt.Errorf("int %d overflows %v", n, t)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := toUint64(raw)
		if err != nil {
			return err
		}
		if fv.OverflowUint(n) {
			return fmt.Errorf("uint %d overflows %v", n, t)
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := toFloat64(raw)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	case reflect.String:
		s, err := toString(raw)
		if err != nil {
			return err
		}
		fv.SetString(s)
	case reflect.Bool:
		b, err := toBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 { // []byte: treat as string, not list
			s, err := toString(raw)
			if err != nil {
				return err
			}
			fv.SetBytes([]byte(s))
			return nil
		}
		return setSlice(fv, raw)
	case reflect.Map:
		return setMap(fv, raw)
	default:
		return fmt.Errorf("unsupported field type %v", t)
	}
	return nil
}

func setSlice(fv reflect.Value, raw any) error {
	items, err := toItems(raw)
	if err != nil {
		return err
	}
	elemT := fv.Type().Elem()
	out := reflect.MakeSlice(fv.Type(), len(items), len(items))
	for i, it := range items {
		ev := reflect.New(elemT).Elem()
		if err := setValue(ev, it); err != nil {
			return fmt.Errorf("[%d]: %w", i, err)
		}
		out.Index(i).Set(ev)
	}
	fv.Set(out)
	return nil
}

func setMap(fv reflect.Value, raw any) error {
	entries, err := toEntries(raw)
	if err != nil {
		return err
	}
	keyT := fv.Type().Key()
	valT := fv.Type().Elem()
	out := reflect.MakeMapWithSize(fv.Type(), len(entries))
	for k, v := range entries {
		kv := reflect.New(keyT).Elem()
		if err := setValue(kv, k); err != nil {
			return fmt.Errorf("key %v: %w", k, err)
		}
		vv := reflect.New(valT).Elem()
		if err := setValue(vv, v); err != nil {
			return fmt.Errorf("value for key %v: %w", k, err)
		}
		out.SetMapIndex(kv, vv)
	}
	fv.Set(out)
	return nil
}

// ---------------------------------------------------------------------------
// scalar coercers
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
	case uint, uint8, uint16, uint32, uint64:
		return int64(reflect.ValueOf(v).Uint()), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
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
		n := reflect.ValueOf(v).Int()
		if n < 0 {
			return 0, fmt.Errorf("negative value %d for uint", n)
		}
		return uint64(n), nil
	case float64:
		if v < 0 {
			return 0, fmt.Errorf("negative value %v for uint", v)
		}
		return uint64(v), nil
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
		return float64(reflect.ValueOf(v).Int()), nil
	case uint, uint8, uint16, uint32, uint64:
		return float64(reflect.ValueOf(v).Uint()), nil
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
		return time.Duration(reflect.ValueOf(v).Int()), nil
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
// collection adapters
// ---------------------------------------------------------------------------

// toItems turns raw into a slice of element-level raws. A string is treated as
// JSON when it starts with '[', otherwise as a comma-separated list. YAML
// already yields []any.
func toItems(raw any) ([]any, error) {
	switch v := raw.(type) {
	case []any:
		return v, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		if strings.HasPrefix(s, "[") {
			var out []any
			if err := json.Unmarshal([]byte(s), &out); err != nil {
				return nil, fmt.Errorf("invalid JSON list %q: %w", s, err)
			}
			return out, nil
		}
		parts := strings.Split(s, ",")
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = strings.TrimSpace(p)
		}
		return out, nil
	}
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

// toEntries turns raw into key/value raws. A string is JSON when it starts with
// '{', otherwise "k=v,k2=v2". YAML yields map[string]any.
func toEntries(raw any) (map[any]any, error) {
	switch v := raw.(type) {
	case map[string]any:
		out := make(map[any]any, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		if strings.HasPrefix(s, "{") {
			var m map[string]any
			if err := json.Unmarshal([]byte(s), &m); err != nil {
				return nil, fmt.Errorf("invalid JSON map %q: %w", s, err)
			}
			out := make(map[any]any, len(m))
			for k, val := range m {
				out[k] = val
			}
			return out, nil
		}
		out := map[any]any{}
		for _, part := range strings.Split(s, ",") {
			k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
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
