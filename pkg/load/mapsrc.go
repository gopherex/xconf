package load

import "github.com/gopherex/xconf"

// MapSource resolves values from a nested map keyed by field path.
//
//	load.FromMap(map[string]any{
//	    "Port": 9090,
//	    "Redis": map[string]any{"Addr": "localhost:6379"},
//	})
type MapSource struct {
	Data map[string]any
}

// FromMap wraps data as a Source.
func FromMap(data map[string]any) Source {
	return MapSource{Data: data}
}

func (m MapSource) Lookup(_ xconf.FieldDesc, path []string) (any, bool, error) {
	var cur any = m.Data
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		v, ok := mm[p]
		if !ok {
			return nil, false, nil
		}
		cur = v
	}
	return cur, true, nil
}
