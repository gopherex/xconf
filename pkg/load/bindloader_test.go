package load_test

import (
	"errors"
	"testing"

	"github.com/gopherex/xconf"
	"github.com/gopherex/xconf/pkg/load"
	"github.com/gopherex/xconf/pkg/validate"
)

type extCfg struct {
	Token string
	Count int
}

var extCalls int

func loadExt() (*extCfg, error) {
	extCalls++
	return &extCfg{Token: "from-loader", Count: 42}, nil
}

func TestRuntimeBindLoader(t *testing.T) {
	extCalls = 0
	sub := xconf.WithLoader(
		xconf.GroupAs[extCfg]("Ext",
			xconf.String("Token"),
			xconf.Int("Count"),
		),
		loadExt,
	)
	schema := xconf.Define("Root",
		xconf.Int("Port").Default(8080),
		xconf.Embed("Ext", sub),
	)
	type Root struct {
		Port int
		Ext  extCfg
	}
	var r Root
	if err := load.Load(schema, &r); err != nil {
		t.Fatalf("load: %v", err)
	}
	if extCalls != 1 {
		t.Errorf("loader call count: %d", extCalls)
	}
	if r.Port != 8080 {
		t.Errorf("Port default lost: %d", r.Port)
	}
	if r.Ext.Token != "from-loader" || r.Ext.Count != 42 {
		t.Errorf("Ext from loader: %+v", r.Ext)
	}
}

func TestRuntimeBindLoaderError(t *testing.T) {
	failing := func() (*extCfg, error) { return nil, errors.New("boom") }
	sub := xconf.WithLoader(
		xconf.GroupAs[extCfg]("Ext", xconf.String("Token")),
		failing,
	)
	schema := xconf.Define("Root", xconf.Embed("Ext", sub))
	type Root struct{ Ext extCfg }
	var r Root
	err := load.Load(schema, &r)
	if err == nil {
		t.Fatalf("expected loader error")
	}
}

func TestValidateAfterLoad(t *testing.T) {
	schema := xconf.Define("C",
		xconf.Int("Port").Validate(validate.Range(1, 65535)),
	)
	type C struct{ Port int }
	c := C{Port: 70000} // out of range
	if err := load.Validate(schema, &c); err == nil {
		t.Errorf("expected validator failure")
	}
	c.Port = 8080
	if err := load.Validate(schema, &c); err != nil {
		t.Errorf("validator pass: %v", err)
	}
}
