package flags_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/flags"
)

func TestProtobufEnumChoiceFromFlags(t *testing.T) {
	type config struct {
		Mode descriptorpb.FieldDescriptorProto_Type `json:"mode"`
	}
	s, err := sp.ReflectType[config](sp.ID("test", "enum_flags", sp.Ver(1, 0, 0)))
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]x.Path{"mode": {"mode"}}
	got, err := x.LoadAs[config](context.Background(), s, flags.New([]string{"--mode=2"}, bindings))
	if err != nil || got.Mode != descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
		t.Fatalf("enum flag: %+v %v", got, err)
	}
	for _, value := range []string{"999", "2.5", "TYPE_FLOAT"} {
		if _, err := x.Load(context.Background(), s, flags.New([]string{"--mode=" + value}, bindings)); err == nil {
			t.Fatalf("invalid enum flag accepted: %s", value)
		}
	}
}

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
