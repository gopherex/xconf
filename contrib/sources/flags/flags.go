// Package flags reads explicit --name=value or --name value arguments.
// Only provided flags produce values; schema defaults run after all sources merge.
package flags

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/env"
)

type Source struct {
	args     []string
	bindings map[string]x.Path
}

// New uses explicit bindings; it never reads os.Args or modifies global flag state.
func New(args []string, bindings map[string]x.Path) *Source {
	s := &Source{args: append([]string(nil), args...), bindings: map[string]x.Path{}}
	for k, v := range bindings {
		s.bindings[k] = append(x.Path(nil), v...)
	}
	return s
}
func (s *Source) Name() string { return "flags" }
func (s *Source) Read(ctx context.Context, schema *sp.Schema) (x.Layer, error) {
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	vars := map[string]string{}
	options := []env.Option{env.Prefix("__XCONF_UNUSED_")}
	for name, path := range s.bindings {
		options = append(options, env.Bind(name, path...))
	}
	for i := 0; i < len(s.args); i++ {
		arg := s.args[i]
		if !strings.HasPrefix(arg, "--") {
			return x.Layer{}, errors.New("unexpected positional argument")
		}
		name, value, equals := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if _, ok := s.bindings[name]; !ok {
			return x.Layer{}, fmt.Errorf("unknown flag %q", name)
		}
		if !equals {
			if i+1 >= len(s.args) || strings.HasPrefix(s.args[i+1], "--") {
				return x.Layer{}, fmt.Errorf("flag %q requires a value", name)
			}
			i++
			value = s.args[i]
		}
		vars[name] = value
	}
	return env.Decode(schema, vars, options...)
}
