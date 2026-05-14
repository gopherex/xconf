package app_test

import (
	"testing"
	"time"

	"github.com/gopherex/xconf"
	"github.com/gopherex/xconf/example/app"
	"github.com/gopherex/xconf/example/redislib"
	"github.com/gopherex/xconf/pkg/load"
)

func TestAppSchemaDescribe(t *testing.T) {
	d := app.Schema.Describe()

	if d.Name != "AppConfig" || d.Kind != xconf.KindGroup {
		t.Fatalf("root: got name=%q kind=%v", d.Name, d.Kind)
	}
	if len(d.Children) != 5 {
		t.Fatalf("expected 5 top-level fields, got %d", len(d.Children))
	}

	port := d.Children[0]
	if port.Name != "Port" || port.Kind != xconf.KindInt || port.GoType != "int" {
		t.Errorf("port: %+v", port)
	}
	if port.Env != "PORT" {
		t.Errorf("port env: got %q want PORT", port.Env)
	}
	if !port.HasDefault || port.Default.(int) != 8080 {
		t.Errorf("port default: %+v", port)
	}

	dsn := d.Children[1]
	if dsn.Env != "DB_DSN" {
		t.Errorf("dsn env override lost: %q", dsn.Env)
	}

	hosts := d.Children[2]
	if hosts.Kind != xconf.KindSlice || hosts.ElemGoType != "string" || hosts.EnvSplit != "," {
		t.Errorf("hosts: %+v", hosts)
	}
	if hosts.Env != "ALLOWED_HOSTS" {
		t.Errorf("hosts env: got %q want ALLOWED_HOSTS", hosts.Env)
	}

	limits := d.Children[3]
	if limits.Kind != xconf.KindMap || limits.KeyGoType != "string" || limits.ElemGoType != "int" {
		t.Errorf("rateLimits: %+v", limits)
	}
	if limits.EnvSplit != "," || limits.KVSplit != "=" {
		t.Errorf("rateLimits split: %+v", limits)
	}
	if limits.Env != "RATE_LIMITS" {
		t.Errorf("rateLimits env: got %q want RATE_LIMITS", limits.Env)
	}

	redis := d.Children[4]
	if redis.Name != "Redis" || redis.Kind != xconf.KindGroup {
		t.Fatalf("redis embed: %+v", redis)
	}
	wantBind := "github.com/gopherex/xconf/example/redislib.Config"
	if redis.BindType != wantBind {
		t.Errorf("redis bindType: got %q want %q", redis.BindType, wantBind)
	}
	if len(redis.Children) != 4 {
		t.Fatalf("redis children: %d", len(redis.Children))
	}

	wantEnvs := []string{"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "REDIS_TIMEOUT"}
	for i, want := range wantEnvs {
		if got := redis.Children[i].Env; got != want {
			t.Errorf("redis child %d env: got %q want %q", i, got, want)
		}
	}

	timeout := redis.Children[3]
	if timeout.Kind != xconf.KindDuration || timeout.Default.(time.Duration) != 5*time.Second {
		t.Errorf("redis timeout: %+v", timeout)
	}

	// Sanity: original redislib schema preserved its own name.
	if redislib.ConfigSchema.Describe().Name != "Config" {
		t.Errorf("Embed mutated source schema")
	}
}

// Exercises the generated app.LoadFromEnv (zero-reflection path).
func TestGeneratedLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DB_DSN", "postgres://gen-env/x")
	t.Setenv("ALLOWED_HOSTS", "a.com,b.com")
	t.Setenv("RATE_LIMITS", "r=10,w=20")
	t.Setenv("REDIS_ADDR", "env-redis:6379")
	t.Setenv("REDIS_TIMEOUT", "3s")

	cfg, err := app.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Port != 9090 || cfg.DSN != "postgres://gen-env/x" {
		t.Errorf("scalars: %+v", cfg)
	}
	if len(cfg.AllowedHosts) != 2 || cfg.AllowedHosts[0] != "a.com" {
		t.Errorf("hosts: %v", cfg.AllowedHosts)
	}
	if cfg.RateLimits["r"] != 10 || cfg.RateLimits["w"] != 20 {
		t.Errorf("limits: %v", cfg.RateLimits)
	}
	if cfg.Redis.Addr != "env-redis:6379" || cfg.Redis.Timeout != 3*time.Second {
		t.Errorf("redis: %+v", cfg.Redis)
	}
	// Default that was not overridden by env.
	if cfg.Redis.DB != 0 {
		t.Errorf("redis db default: %d", cfg.Redis.DB)
	}
}

// Exercises the generated app.Load wrapper end-to-end.
func TestGeneratedLoad(t *testing.T) {
	envs := map[string]string{
		"PORT":          "7777",
		"DB_DSN":        "postgres://gen/x",
		"REDIS_ADDR":    "rds:6379",
		"REDIS_TIMEOUT": "2s",
	}
	cfg, err := app.Load(load.FromEnv(envs))
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	if cfg.Port != 7777 || cfg.DSN != "postgres://gen/x" {
		t.Errorf("scalars: %+v", cfg)
	}
	if cfg.Redis.Addr != "rds:6379" || cfg.Redis.Timeout != 2*time.Second {
		t.Errorf("redis: %+v", cfg.Redis)
	}
}
