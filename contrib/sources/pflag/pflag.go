// Package pflag captures explicitly changed flags from pflag and Cobra commands.
package pflag

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	flag "github.com/spf13/pflag"
	"strconv"
	"strings"
	"time"
)

// ValueDecoder supports custom pflag.Value implementations. Return owned raw data.
type ValueDecoder func(*flag.Flag) (any, error)
type Option func(*settings)
type settings struct {
	id     string
	decode ValueDecoder
}

func Name(id string) Option              { return func(s *settings) { s.id = id } }
func DecodeValue(fn ValueDecoder) Option { return func(s *settings) { s.decode = fn } }

type Source struct{ memory *x.Memory }

// New captures a FlagSet after parsing, without modifying it. Only bound Changed
// flags are included. For Cobra pass cmd.Flags() from RunE (it includes inherited
// persistent flags after parsing). Rebuild the source if the FlagSet changes.
func New(set *flag.FlagSet, bindings map[string]x.Path, opts ...Option) (*Source, error) {
	conf := settings{id: "pflag"}
	for _, o := range opts {
		o(&conf)
	}
	if set == nil {
		return nil, errors.New("nil flag set")
	}
	layer := x.Layer{Values: map[string]any{}, Locations: map[string]x.Location{}}
	paths := map[string]bool{}
	// Validate bindings even when flags were not supplied.
	for name, path := range bindings {
		if set.Lookup(name) == nil || len(path) == 0 {
			return nil, fmt.Errorf("invalid flag binding %q", name)
		}
		key := path.String()
		for p := range paths {
			if p == key || strings.HasPrefix(key, p+"/") || strings.HasPrefix(p, key+"/") {
				return nil, errors.New("overlapping flag bindings")
			}
		}
		paths[key] = true
	}
	var readErr error
	set.Visit(func(f *flag.Flag) {
		if readErr != nil {
			return
		}
		path, ok := bindings[f.Name]
		if !ok {
			return
		}
		var value any
		var err error
		if conf.decode != nil {
			value, err = conf.decode(f)
		} else {
			value, err = readValue(set, f)
		}
		if err != nil {
			readErr = fmt.Errorf("flag %s: %w", f.Name, err)
			return
		}
		dst := layer.Values
		for _, key := range path[:len(path)-1] {
			if dst[key] == nil {
				dst[key] = map[string]any{}
			}
			dst = dst[key].(map[string]any)
		}
		dst[path[len(path)-1]] = value
		layer.Locations[path.String()] = x.Location{Name: "--" + f.Name}
	})
	if readErr != nil {
		return nil, readErr
	}
	memory, err := x.NewMemory(conf.id, nil)
	if err != nil {
		return nil, err
	}
	if err = memory.Set(layer); err != nil {
		return nil, err
	}
	return &Source{memory: memory}, nil
}
func (s *Source) Name() string { return s.memory.Name() }
func (s *Source) Read(ctx context.Context, schema *sp.Schema) (x.Layer, error) {
	return s.memory.Read(ctx, schema)
}
func readValue(set *flag.FlagSet, f *flag.Flag) (any, error) {
	switch f.Value.Type() {
	case "stringToString":
		return set.GetStringToString(f.Name)
	case "stringToInt":
		return set.GetStringToInt(f.Name)
	case "stringToInt64":
		return set.GetStringToInt64(f.Name)
	}
	if slice, ok := f.Value.(flag.SliceValue); ok {
		typ := strings.TrimSuffix(f.Value.Type(), "Slice")
		if typ == "stringArray" {
			typ = "string"
		}
		raw := slice.GetSlice()
		values := make([]any, len(raw))
		for i, v := range raw {
			value, err := scalar(typ, v)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return values, nil
	}
	if getter, ok := f.Value.(interface{ Get() any }); ok {
		return getter.Get(), nil
	}
	return scalar(f.Value.Type(), f.Value.String())
}
func scalar(typ, raw string) (any, error) {
	switch typ {
	case "string", "ip", "ipNet", "ipMask":
		return raw, nil
	case "bool":
		return strconv.ParseBool(raw)
	case "int", "int8", "int16", "int32", "int64", "count":
		return strconv.ParseInt(raw, 10, 64)
	case "uint", "uint8", "uint16", "uint32", "uint64":
		return strconv.ParseUint(raw, 10, 64)
	case "float32":
		v, err := strconv.ParseFloat(raw, 32)
		return float32(v), err
	case "float64":
		return strconv.ParseFloat(raw, 64)
	case "duration":
		return time.ParseDuration(raw)
	case "bytesHex":
		return hex.DecodeString(raw)
	case "bytesBase64":
		return base64.StdEncoding.DecodeString(raw)
	default:
		return nil, fmt.Errorf("unsupported flag type %q; supply DecodeValue", typ)
	}
}
