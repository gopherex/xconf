package structconf_test

import (
	"testing"
	"time"

	"github.com/gopherex/xconf/pkg/structconf"
)

// --- all scalar/collection types ---

type Types struct {
	Dur  time.Duration  `mapstructure:"dur" default:"5s"`
	When time.Time      `mapstructure:"when" default:"2020-01-02T03:04:05Z"`
	U    uint16         `mapstructure:"u" default:"42"`
	I64  int64          `mapstructure:"i64" default:"-9"`
	F    float64        `mapstructure:"f" default:"3.14"`
	B    bool           `mapstructure:"b" default:"true"`
	M    map[string]int `mapstructure:"m" default:"{\"a\":1,\"b\":2}"`
	MS   map[string]int `mapstructure:"ms" default:"x=1,y=2"`
	Raw  []byte         `mapstructure:"raw" default:"hello"`
	Ints []int          `mapstructure:"ints" default:"1,2,3"`
	PtrI *int           `mapstructure:"ptri"`
}

func TestAllTypes(t *testing.T) {
	cfg, err := structconf.Load[Types](structconf.WithEnvVars(map[string]string{}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Dur != 5*time.Second {
		t.Errorf("dur=%v", cfg.Dur)
	}
	if !cfg.When.Equal(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("when=%v", cfg.When)
	}
	if cfg.U != 42 || cfg.I64 != -9 || cfg.F != 3.14 || !cfg.B {
		t.Errorf("scalars: %+v", cfg)
	}
	if cfg.M["a"] != 1 || cfg.M["b"] != 2 {
		t.Errorf("json map=%v", cfg.M)
	}
	if cfg.MS["x"] != 1 || cfg.MS["y"] != 2 {
		t.Errorf("kv map=%v", cfg.MS)
	}
	if string(cfg.Raw) != "hello" {
		t.Errorf("bytes=%q", string(cfg.Raw))
	}
	if len(cfg.Ints) != 3 || cfg.Ints[2] != 3 {
		t.Errorf("ints=%v", cfg.Ints)
	}
	if cfg.PtrI != nil {
		t.Errorf("ptri should stay nil, got %v", *cfg.PtrI)
	}
}

func TestPointerSetWhenProvided(t *testing.T) {
	cfg, err := structconf.Load[Types](structconf.WithEnvVars(map[string]string{"PTRI": "7"}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PtrI == nil || *cfg.PtrI != 7 {
		t.Errorf("ptri=%v", cfg.PtrI)
	}
}

// --- squash / embedded ---

type Base struct {
	X string `mapstructure:"x" default:"ix"`
}
type Embeds struct {
	Base `mapstructure:",squash"`
	Anon              // anonymous, no tag => squashed
	Top  string `mapstructure:"top" default:"t"`
}
type Anon struct {
	Y string `mapstructure:"y" default:"iy"`
}

func TestSquash(t *testing.T) {
	cfg, err := structconf.Load[Embeds](structconf.WithEnvVars(map[string]string{"X": "ex"}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.X != "ex" { // flat env name, no prefix from embedded
		t.Errorf("squash X=%q want ex", cfg.X)
	}
	if cfg.Y != "iy" {
		t.Errorf("anon Y=%q want iy", cfg.Y)
	}
	if cfg.Top != "t" {
		t.Errorf("top=%q", cfg.Top)
	}
}

// --- dive + unique ---

type Coll struct {
	Hosts   []string `mapstructure:"hosts" validate:"dive,hostname"`
	Ports   []int    `mapstructure:"ports" validate:"unique,dive,min=1,max=65535"`
	Modes   []string `mapstructure:"modes" validate:"dive,oneof=a b c"`
}

func TestDiveOK(t *testing.T) {
	_, err := structconf.Load[Coll](structconf.WithEnvVars(map[string]string{
		"HOSTS": "a.com,b.com",
		"PORTS": "80,443",
		"MODES": "a,b",
	}))
	if err != nil {
		t.Fatalf("should pass: %v", err)
	}
}

func TestDiveElementFails(t *testing.T) {
	_, err := structconf.Load[Coll](structconf.WithEnvVars(map[string]string{
		"PORTS": "80,70000", // element out of range
	}))
	if err == nil {
		t.Fatal("expected dive element failure")
	}
}

func TestUniqueFails(t *testing.T) {
	_, err := structconf.Load[Coll](structconf.WithEnvVars(map[string]string{
		"PORTS": "80,80",
	}))
	if err == nil {
		t.Fatal("expected unique failure")
	}
}

// --- cross-field ---

type Cross struct {
	Pass    string `mapstructure:"pass" default:"secret"`
	Confirm string `mapstructure:"confirm" default:"secret" validate:"eqfield=Pass"`
	Mode    string `mapstructure:"mode" default:"tls"`
	Cert    string `mapstructure:"cert" validate:"required_if=Mode tls"`
}

func TestEqfieldOK(t *testing.T) {
	_, err := structconf.Load[Cross](structconf.WithEnvVars(map[string]string{"CERT": "x"}))
	if err != nil {
		t.Fatalf("eqfield should pass: %v", err)
	}
}

func TestEqfieldFails(t *testing.T) {
	_, err := structconf.Load[Cross](structconf.WithEnvVars(map[string]string{
		"CONFIRM": "other", "CERT": "x",
	}))
	if err == nil {
		t.Fatal("expected eqfield failure")
	}
}

func TestRequiredIfFires(t *testing.T) {
	_, err := structconf.Load[Cross](structconf.WithEnvVars(map[string]string{})) // Mode=tls, Cert empty
	if err == nil {
		t.Fatal("expected required_if failure")
	}
}

func TestRequiredIfSkipped(t *testing.T) {
	_, err := structconf.Load[Cross](structconf.WithEnvVars(map[string]string{"MODE": "plain"}))
	if err != nil {
		t.Fatalf("required_if should not fire: %v", err)
	}
}

// --- string/format validators ---

type Formats struct {
	ID    string `mapstructure:"id" default:"550e8400-e29b-41d4-a716-446655440000" validate:"uuid"`
	Net   string `mapstructure:"net" default:"10.0.0.0/8" validate:"cidr"`
	HW    string `mapstructure:"hw" default:"01:23:45:67:89:ab" validate:"mac"`
	Site  string `mapstructure:"site" default:"db.example.com" validate:"fqdn"`
	Doc   string `mapstructure:"doc" default:"{\"k\":1}" validate:"json"`
	B64   string `mapstructure:"b64" default:"aGVsbG8=" validate:"base64"`
	Code  string `mapstructure:"code" default:"abc123" validate:"alphanum"`
	Num   string `mapstructure:"num" default:"-12.5" validate:"numeric"`
	Phone string `mapstructure:"phone" default:"+14155550123" validate:"e164"`
	Day   string `mapstructure:"day" default:"2024-06-05" validate:"datetime=2006-01-02"`
}

func TestFormatsOK(t *testing.T) {
	if _, err := structconf.Load[Formats](structconf.WithEnvVars(map[string]string{})); err != nil {
		t.Fatalf("formats should pass: %v", err)
	}
}

func TestFormatFails(t *testing.T) {
	cases := map[string]string{
		"ID": "not-a-uuid", "NET": "999.0.0.0/8", "HW": "zz", "SITE": "nodot",
		"DOC": "{bad", "B64": "!!!", "CODE": "a b", "NUM": "x", "PHONE": "0123", "DAY": "06/05/2024",
	}
	for k, v := range cases {
		if _, err := structconf.Load[Formats](structconf.WithEnvVars(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q should fail validation", k, v)
		}
	}
}

// --- oneof with quoted multi-word ---

type Quoted struct {
	V string `mapstructure:"v" default:"hot pink" validate:"oneof='hot pink' blue 'sea green'"`
}

func TestOneofQuoted(t *testing.T) {
	if _, err := structconf.Load[Quoted](structconf.WithEnvVars(map[string]string{})); err != nil {
		t.Errorf("quoted oneof should pass: %v", err)
	}
	if _, err := structconf.Load[Quoted](structconf.WithEnvVars(map[string]string{"V": "red"})); err == nil {
		t.Error("quoted oneof should fail for 'red'")
	}
}

// --- unknown rule errors ---

type Unknown struct {
	V string `mapstructure:"v" default:"x" validate:"bogusrule"`
}

func TestUnknownRuleErrors(t *testing.T) {
	_, err := structconf.Load[Unknown](structconf.WithEnvVars(map[string]string{}))
	if err == nil {
		t.Fatal("unknown rule must error")
	}
}
