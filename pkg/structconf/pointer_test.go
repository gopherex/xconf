package structconf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gopherex/xconf/pkg/structconf"
)

type section struct {
	Addr string `mapstructure:"addr" validate:"required"`
	Port int    `mapstructure:"port" default:"9000"`
}

type optionalCfg struct {
	Name   string   `mapstructure:"name" default:"x"`
	Listen *section `mapstructure:"listen"`
	Link   *section `mapstructure:"link"`
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A pointer section present in the file is materialized: values bound,
// defaults applied, validation enforced. An absent one stays nil and its
// required fields do not fire.
func TestPointerSectionPresence(t *testing.T) {
	path := writeTemp(t, "listen:\n  addr: \"0.0.0.0:1\"\n")

	cfg, err := structconf.Load[optionalCfg](
		structconf.WithYAMLFile(path),
		structconf.WithEnvVars(map[string]string{}),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Listen == nil || cfg.Listen.Addr != "0.0.0.0:1" {
		t.Fatalf("present section not populated: %+v", cfg.Listen)
	}
	if cfg.Listen.Port != 9000 {
		t.Fatalf("default inside present section not applied: %+v", cfg.Listen)
	}
	if cfg.Link != nil {
		t.Fatalf("absent section must stay nil, got %+v", cfg.Link)
	}
}

// With no sections at all, nothing is materialized and nothing validates.
func TestPointerSectionAllAbsent(t *testing.T) {
	path := writeTemp(t, "name: y\n")

	cfg, err := structconf.Load[optionalCfg](
		structconf.WithYAMLFile(path),
		structconf.WithEnvVars(map[string]string{}),
	)
	if err != nil {
		t.Fatalf("absent sections triggered validation: %v", err)
	}
	if cfg.Listen != nil || cfg.Link != nil {
		t.Fatalf("sections materialized from nothing: %+v", cfg)
	}
}

// An env var under the section path materializes it too.
func TestPointerSectionFromEnv(t *testing.T) {
	cfg, err := structconf.Load[optionalCfg](
		structconf.WithEnvVars(map[string]string{"LINK_ADDR": "srv1:9000"}),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Link == nil || cfg.Link.Addr != "srv1:9000" || cfg.Link.Port != 9000 {
		t.Fatalf("env-materialized section wrong: %+v", cfg.Link)
	}
	if cfg.Listen != nil {
		t.Fatalf("untouched section materialized: %+v", cfg.Listen)
	}
}

// A present section with a missing required field still fails — presence
// switches validation on, not off.
func TestPointerSectionStillValidates(t *testing.T) {
	path := writeTemp(t, "listen:\n  port: 80\n")

	if _, err := structconf.Load[optionalCfg](
		structconf.WithYAMLFile(path),
		structconf.WithEnvVars(map[string]string{}),
	); err == nil {
		t.Fatal("present section skipped required validation")
	}
}
