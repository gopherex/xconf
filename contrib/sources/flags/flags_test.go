package flags_test

import (
	"context"
	"testing"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/flags"
)

func TestFlagsOnlySuppliedValues(t *testing.T) {
	s := sp.NewSchema(sp.ID("test", "flags", sp.Ver(1, 0, 0))).Coerce().Fields(sp.Int64("n").Default(7), sp.Bool("enabled").Default(true)).MustBuild()
	bindings := map[string]x.Path{"n": {"n"}, "enabled": {"enabled"}}
	v, err := x.LoadAs[map[string]any](context.Background(), s, flags.New([]string{"--enabled=false"}, bindings))
	if err != nil || v["n"] != int64(7) || v["enabled"] != false {
		t.Fatal(v, err)
	}
	for _, args := range [][]string{{"--unknown=1"}, {"--n"}, {"positional"}} {
		if _, err := x.Load(context.Background(), s, flags.New(args, bindings)); err == nil {
			t.Fatal(args)
		}
	}
}
