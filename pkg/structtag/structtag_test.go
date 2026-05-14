package structtag_test

import (
	"testing"
	"time"

	"github.com/gopherex/xconf/pkg/load"
	"github.com/gopherex/xconf/pkg/structtag"
)

type DBCfg struct {
	Host string `xconf:"default=localhost"`
	Port int    `xconf:"default=5432"`
}

type AppCfg struct {
	Port    int            `xconf:"default=8080"`
	DSN     string         `xconf:"env=DB_DSN,required,desc=postgres dsn"`
	Tags    []string       `xconf:"split=|"`
	Limits  map[string]int `xconf:"split=;,kv=:"`
	Timeout time.Duration  `xconf:"default=2s"`
	DB      DBCfg
	Skipped string `xconf:"skip"`
}

func TestSchemaFromStruct(t *testing.T) {
	schema, err := structtag.SchemaFromStruct[AppCfg]("App")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	d := schema.Describe()
	if d.Name != "App" {
		t.Errorf("name: %s", d.Name)
	}
	// Skipped must be excluded.
	for _, c := range d.Children {
		if c.Name == "Skipped" {
			t.Errorf("Skipped field included")
		}
	}
}

func TestStructTagLoad(t *testing.T) {
	schema, err := structtag.SchemaFromStruct[AppCfg]("App")
	if err != nil {
		t.Fatal(err)
	}
	envs := map[string]string{
		"DB_DSN":  "postgres://tag/x",
		"TAGS":    "a|b|c",
		"LIMITS":  "r:5;w:9",
		"TIMEOUT": "750ms",
		"DB_HOST": "db.local",
	}
	var cfg AppCfg
	if err := load.Load(schema, &cfg, load.FromEnv(envs)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("default Port: %d", cfg.Port)
	}
	if cfg.DSN != "postgres://tag/x" {
		t.Errorf("DSN: %q", cfg.DSN)
	}
	want := []string{"a", "b", "c"}
	if len(cfg.Tags) != 3 || cfg.Tags[2] != want[2] {
		t.Errorf("tags: %v", cfg.Tags)
	}
	if cfg.Limits["r"] != 5 || cfg.Limits["w"] != 9 {
		t.Errorf("limits: %v", cfg.Limits)
	}
	if cfg.Timeout != 750*time.Millisecond {
		t.Errorf("timeout: %v", cfg.Timeout)
	}
	if cfg.DB.Host != "db.local" || cfg.DB.Port != 5432 {
		t.Errorf("db: %+v", cfg.DB)
	}
}
