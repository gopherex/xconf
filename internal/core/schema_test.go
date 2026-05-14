package core

import (
	"strings"
	"testing"
	"time"
)

func TestEnvPrefixOverride(t *testing.T) {
	s := Define("App",
		Group("DB", String("Host"), Int("Port")).EnvPrefix("MYDB"),
	)
	d := s.Describe()
	if got := d.Children[0].Children[0].Env; got != "MYDB_HOST" {
		t.Errorf("EnvPrefix override: got %q want MYDB_HOST", got)
	}
}

func TestEnvPrefixDisabled(t *testing.T) {
	s := Define("App",
		Group("DB", String("Host")).EnvPrefix(""),
	)
	if got := s.Describe().Children[0].Children[0].Env; got != "HOST" {
		t.Errorf("empty prefix: got %q want HOST", got)
	}
}

func TestGroupAsPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for unnamed type parameter")
		}
	}()
	_ = GroupAs[struct{ X int }]("Anon")
}

func TestEmbedIndependence(t *testing.T) {
	type Cfg struct{ Addr string }
	sub := GroupAs[Cfg]("Sub", String("Addr"))
	embedded := Embed("Renamed", sub)

	if sub.Describe().Name != "Sub" {
		t.Errorf("Embed mutated source")
	}
	if embedded.Describe().Name != "Renamed" {
		t.Errorf("Embed did not rename")
	}
	if embedded.Describe().Children[0].Env != "RENAMED_ADDR" {
		t.Errorf("embedded child env not re-derived: %q", embedded.Describe().Children[0].Env)
	}
}

type loaderCfg struct{ X int }

func myLoader() (*loaderCfg, error) { return nil, nil }

func TestWithLoader(t *testing.T) {
	s := WithLoader(GroupAs[loaderCfg]("Sub", Int("X")), myLoader)
	if s.bindLoader == "" || !strings.Contains(s.bindLoader, "myLoader") {
		t.Errorf("bindLoader not captured: %q", s.bindLoader)
	}
}

func TestNumericConstructorsAllProduceDescribe(t *testing.T) {
	cases := []Node{
		Int8("a"), Int16("b"), Int32("c"), Int64("d"),
		Uint("e"), Uint8("f"), Uint16("g"), Uint32("h"), Uint64("i"),
		Float32("j"), Float64("k"),
		Bytes("l"), Time("m"),
	}
	for _, n := range cases {
		d := n.describe()
		if d.Name == "" || d.GoType == "" || d.Kind == KindInvalid {
			t.Errorf("bad describe: %+v", d)
		}
	}
}

func TestSliceMapChainCoverage(t *testing.T) {
	s := Slice[int]("Xs").Default([]int{1}).Env("XS").Required().Description("desc")
	d := s.describe()
	if !d.HasDefault || !d.Required || d.Env != "XS" || d.Description != "desc" {
		t.Errorf("slice chain: %+v", d)
	}

	m := Map[string, int]("Lim").Default(map[string]int{"a": 1}).
		Env("LIM").EnvSplit(",").KVSplit("=").Required().Description("d")
	md := m.describe()
	if !md.HasDefault || !md.Required || md.EnvSplit != "," || md.KVSplit != "=" {
		t.Errorf("map chain: %+v", md)
	}
}

func TestTimeDurationInSlice(t *testing.T) {
	s := Slice[time.Duration]("Ts").describe()
	if s.ElemKind != KindDuration || s.ElemGoType != "time.Duration" {
		t.Errorf("duration slice elem: %+v", s)
	}
}
