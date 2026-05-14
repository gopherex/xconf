package load_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gopherex/xconf"
	"github.com/gopherex/xconf/pkg/load"
)

func TestYAMLFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	body := []byte("Port: 7000\nDSN: 'postgres://yaml/x'\nRedis:\n  Addr: 'yaml-redis:6379'\n")
	if err := os.WriteFile(p, body, 0600); err != nil {
		t.Fatal(err)
	}
	type Inner struct{ Addr string }
	type C struct {
		Port  int
		DSN   string
		Redis Inner
	}
	s := xconf.Define("C",
		xconf.Int("Port"),
		xconf.String("DSN"),
		xconf.Group("Redis", xconf.String("Addr")),
	)
	src, err := load.FromYAMLFile(p)
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	var c C
	if err := load.Load(s, &c, src); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Port != 7000 || c.DSN != "postgres://yaml/x" || c.Redis.Addr != "yaml-redis:6379" {
		t.Errorf("yaml result: %+v", c)
	}
}

func TestTOMLFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	body := []byte("Port = 7000\nDSN = \"postgres://toml/x\"\n[Redis]\nAddr = \"toml-redis:6379\"\n")
	if err := os.WriteFile(p, body, 0600); err != nil {
		t.Fatal(err)
	}
	type Inner struct{ Addr string }
	type C struct {
		Port  int
		DSN   string
		Redis Inner
	}
	s := xconf.Define("C",
		xconf.Int("Port"),
		xconf.String("DSN"),
		xconf.Group("Redis", xconf.String("Addr")),
	)
	src, err := load.FromTOMLFile(p)
	if err != nil {
		t.Fatalf("toml: %v", err)
	}
	var c C
	if err := load.Load(s, &c, src); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Port != 7000 || c.DSN != "postgres://toml/x" || c.Redis.Addr != "toml-redis:6379" {
		t.Errorf("toml result: %+v", c)
	}
}

func TestYAMLOptionalMissing(t *testing.T) {
	src, err := load.FromYAMLFileOptional("/no/such.yaml")
	if err != nil {
		t.Fatalf("optional yaml: %v", err)
	}
	_ = src
}

func TestTOMLOptionalMissing(t *testing.T) {
	src, err := load.FromTOMLFileOptional("/no/such.toml")
	if err != nil {
		t.Fatalf("optional toml: %v", err)
	}
	_ = src
}
