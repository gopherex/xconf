package structconf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gopherex/xconf/pkg/structconf"
)

type SrvCfg struct {
	Infra struct {
		Postgres struct {
			Host string `mapstructure:"host" default:"localhost"`
			Port int    `mapstructure:"port" default:"5432"`
		} `mapstructure:"postgres"`
	} `mapstructure:"infra"`
	Service struct {
		HTTP struct {
			Addr string `mapstructure:"addr" default:":8080"`
		} `mapstructure:"http"`
	} `mapstructure:"service"`
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestJSONFile(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.json", `{"infra":{"postgres":{"host":"jhost","port":1111}}}`)
	cfg, err := structconf.Load[SrvCfg](structconf.WithJSONFile(p), structconf.WithEnvVars(map[string]string{}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Infra.Postgres.Host != "jhost" || cfg.Infra.Postgres.Port != 1111 {
		t.Errorf("json: %+v", cfg.Infra.Postgres)
	}
}

func TestTOMLFile(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.toml", "[infra.postgres]\nhost = \"thost\"\nport = 2222\n")
	cfg, err := structconf.Load[SrvCfg](structconf.WithTOMLFile(p), structconf.WithEnvVars(map[string]string{}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Infra.Postgres.Host != "thost" || cfg.Infra.Postgres.Port != 2222 {
		t.Errorf("toml: %+v", cfg.Infra.Postgres)
	}
}

func TestWithFileAutoDetect(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "c.yaml", "infra:\n  postgres:\n    host: yhost\n")
	cfg, err := structconf.Load[SrvCfg](structconf.WithFile(p), structconf.WithEnvVars(map[string]string{}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Infra.Postgres.Host != "yhost" {
		t.Errorf("yaml auto: %q", cfg.Infra.Postgres.Host)
	}
}

func TestMissingFileErrors(t *testing.T) {
	if _, err := structconf.Load[SrvCfg](structconf.WithJSONFile("/no/such.json")); err == nil {
		t.Fatal("missing required file should error")
	}
	if _, err := structconf.Load[SrvCfg](
		structconf.WithJSONFileOptional("/no/such.json"),
		structconf.WithEnvVars(map[string]string{}),
	); err != nil {
		t.Fatalf("optional missing file should not error: %v", err)
	}
}

func TestMultiFileMergeAndPrecedence(t *testing.T) {
	dir := t.TempDir()
	base := write(t, dir, "base.yaml", "infra:\n  postgres:\n    host: basehost\n    port: 1\nservice:\n  http:\n    addr: ':1'\n")
	over := write(t, dir, "over.json", `{"infra":{"postgres":{"port":2}}}`)
	// base < over (file order) < env
	cfg, err := structconf.Load[SrvCfg](
		structconf.WithYAMLFile(base),
		structconf.WithJSONFile(over),
		structconf.WithEnvVars(map[string]string{"SERVICE_HTTP_ADDR": ":9"}),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Infra.Postgres.Host != "basehost" { // only in base, survives merge
		t.Errorf("host=%q", cfg.Infra.Postgres.Host)
	}
	if cfg.Infra.Postgres.Port != 2 { // over wins
		t.Errorf("port=%d want 2", cfg.Infra.Postgres.Port)
	}
	if cfg.Service.HTTP.Addr != ":9" { // env wins over file
		t.Errorf("addr=%q want :9", cfg.Service.HTTP.Addr)
	}
}

func TestDotEnvFallbackUnderRealEnv(t *testing.T) {
	dir := t.TempDir()
	env := write(t, dir, ".env", "# comment\nexport INFRA_POSTGRES_HOST=\"dothost\"\nINFRA_POSTGRES_PORT=7777\n")
	// no WithEnvVars => reads real os env; set one real var to prove it wins
	t.Setenv("INFRA_POSTGRES_PORT", "8888")
	cfg, err := structconf.Load[SrvCfg](structconf.WithDotEnv(env))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Infra.Postgres.Host != "dothost" { // from .env (no real override)
		t.Errorf("host=%q want dothost", cfg.Infra.Postgres.Host)
	}
	if cfg.Infra.Postgres.Port != 8888 { // real env beats .env
		t.Errorf("port=%d want 8888 (real env wins)", cfg.Infra.Postgres.Port)
	}
}

func TestConfigPathDiscovery(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.yaml", "infra:\n  postgres:\n    host: cphost\n")
	t.Setenv("CONFIG_PATH", dir)
	cfg, err := structconf.Load[SrvCfg](structconf.WithConfigPath(""), structconf.WithEnvVars(map[string]string{}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Infra.Postgres.Host != "cphost" {
		t.Errorf("host=%q want cphost", cfg.Infra.Postgres.Host)
	}
}

func TestConfigPathMissingIsOK(t *testing.T) {
	t.Setenv("CONFIG_PATH", t.TempDir()) // empty dir, no config.*
	cfg, err := structconf.Load[SrvCfg](structconf.WithConfigPath(""), structconf.WithEnvVars(map[string]string{}))
	if err != nil {
		t.Fatalf("missing config should be fine: %v", err)
	}
	if cfg.Infra.Postgres.Host != "localhost" { // falls back to default
		t.Errorf("host=%q want default localhost", cfg.Infra.Postgres.Host)
	}
}
