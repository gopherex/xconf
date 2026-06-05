package structconf_test

import (
	"fmt"
	"testing"

	"github.com/gopherex/xconf/pkg/structconf"
)

type Postgres struct {
	Host     string `mapstructure:"host" default:"localhost" validate:"required,hostname|ip"`
	Port     int    `mapstructure:"port" default:"5432" validate:"required,min=1,max=65535"`
	Username string `mapstructure:"username" default:"iam" validate:"required"`
	Password string `mapstructure:"password" default:"iam" validate:"required"`
	Database string `mapstructure:"database" default:"iam" validate:"required"`
	SSLMode  string `mapstructure:"sslmode" default:"disable" validate:"oneof=disable require verify-ca verify-full"`
	LogLevel string `mapstructure:"log_level" default:"info" validate:"oneof=debug info warn error"`
}

func (c *Postgres) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.Username, c.Password, c.Host, c.Port, c.Database, c.SSLMode)
}

type HTTP struct {
	Addr            string `mapstructure:"addr" default:":8080" validate:"required"`
	ReadTimeoutSec  int    `mapstructure:"read_timeout_sec" default:"15" validate:"min=1"`
	WriteTimeoutSec int    `mapstructure:"write_timeout_sec" default:"30" validate:"min=1"`
	ShutdownSec     int    `mapstructure:"shutdown_sec" default:"15" validate:"min=1"`
}

type Logger struct {
	Level  string `mapstructure:"level" default:"info" validate:"oneof=debug info warn error"`
	Format string `mapstructure:"format" default:"json" validate:"oneof=json text"`
}

type CORS struct {
	AllowedOrigins []string `mapstructure:"allowed_origins" default:"[\"*\"]"`
}

type Auth struct {
	DefaultEnvironment string `mapstructure:"default_environment" default:"live" validate:"required"`
	AccessTTLSec       int    `mapstructure:"access_ttl_sec" default:"1800" validate:"min=60"`
	RefreshTTLSec      int    `mapstructure:"refresh_ttl_sec" default:"2592000" validate:"min=60"`
}

type Infrastructure struct {
	Postgres Postgres `mapstructure:"postgres"`
}

type Service struct {
	HTTP   HTTP   `mapstructure:"http"`
	Logger Logger `mapstructure:"logger"`
	CORS   CORS   `mapstructure:"cors"`
	Auth   Auth   `mapstructure:"auth"`
}

type Config struct {
	Infra   Infrastructure `mapstructure:"infra"`
	Service Service        `mapstructure:"service"`
}

func TestDefaultsOnly(t *testing.T) {
	cfg, err := structconf.Load[Config](structconf.WithEnvVars(map[string]string{}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Infra.Postgres.Host; got != "localhost" {
		t.Errorf("host = %q, want localhost", got)
	}
	if got := cfg.Infra.Postgres.Port; got != 5432 {
		t.Errorf("port = %d, want 5432", got)
	}
	if got := cfg.Infra.Postgres.DSN(); got != "postgres://iam:iam@localhost:5432/iam?sslmode=disable" {
		t.Errorf("dsn = %q", got)
	}
	if got := cfg.Service.HTTP.Addr; got != ":8080" {
		t.Errorf("addr = %q, want :8080", got)
	}
	if got := cfg.Service.CORS.AllowedOrigins; len(got) != 1 || got[0] != "*" {
		t.Errorf("allowed_origins = %v, want [*]", got)
	}
	if got := cfg.Service.Auth.RefreshTTLSec; got != 2592000 {
		t.Errorf("refresh_ttl = %d", got)
	}
}

func TestEnvOverride(t *testing.T) {
	cfg, err := structconf.Load[Config](structconf.WithEnvVars(map[string]string{
		"INFRA_POSTGRES_HOST":          "10.0.0.5",
		"INFRA_POSTGRES_PORT":          "6543",
		"SERVICE_HTTP_ADDR":            ":9090",
		"SERVICE_CORS_ALLOWED_ORIGINS": "https://a.com,https://b.com",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Infra.Postgres.Host; got != "10.0.0.5" {
		t.Errorf("host = %q", got)
	}
	if got := cfg.Infra.Postgres.Port; got != 6543 {
		t.Errorf("port = %d", got)
	}
	if got := cfg.Service.HTTP.Addr; got != ":9090" {
		t.Errorf("addr = %q", got)
	}
	if got := cfg.Service.CORS.AllowedOrigins; len(got) != 2 || got[1] != "https://b.com" {
		t.Errorf("origins = %v", got)
	}
}

func TestEnvPrefix(t *testing.T) {
	cfg, err := structconf.Load[Config](
		structconf.WithEnvPrefix("IAM"),
		structconf.WithEnvVars(map[string]string{"IAM_INFRA_POSTGRES_HOST": "db.internal"}),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Infra.Postgres.Host; got != "db.internal" {
		t.Errorf("host = %q", got)
	}
}

func TestValidationFails(t *testing.T) {
	_, err := structconf.Load[Config](structconf.WithEnvVars(map[string]string{
		"INFRA_POSTGRES_PORT": "70000", // > 65535
	}))
	if err == nil {
		t.Fatal("expected validation error for out-of-range port")
	}
}

func TestOneOfFails(t *testing.T) {
	_, err := structconf.Load[Config](structconf.WithEnvVars(map[string]string{
		"INFRA_POSTGRES_SSLMODE": "bogus",
	}))
	if err == nil {
		t.Fatal("expected oneof validation error")
	}
}

func TestHostnameOrIP(t *testing.T) {
	// hostname branch
	if _, err := structconf.Load[Config](structconf.WithEnvVars(map[string]string{
		"INFRA_POSTGRES_HOST": "db.prod.svc",
	})); err != nil {
		t.Errorf("hostname should pass: %v", err)
	}
	// ip branch
	if _, err := structconf.Load[Config](structconf.WithEnvVars(map[string]string{
		"INFRA_POSTGRES_HOST": "192.168.1.1",
	})); err != nil {
		t.Errorf("ip should pass: %v", err)
	}
	// neither
	if _, err := structconf.Load[Config](structconf.WithEnvVars(map[string]string{
		"INFRA_POSTGRES_HOST": "bad host!",
	})); err == nil {
		t.Error("expected hostname|ip failure")
	}
}
