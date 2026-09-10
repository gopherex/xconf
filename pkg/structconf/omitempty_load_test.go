package structconf_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopherex/xconf/pkg/structconf"
)

func TestLoadOmitEmpty(t *testing.T) {
	type config struct {
		URL     string            `mapstructure:"url" validate:"omitempty,url"`
		Port    int               `mapstructure:"port" validate:"omitempty,min=1,max=65535"`
		Count   uint              `mapstructure:"count" validate:"omitempty,min=2"`
		Ratio   float64           `mapstructure:"ratio" validate:"omitempty,min=1"`
		Enabled bool              `mapstructure:"enabled" validate:"omitempty,oneof=true"`
		Delay   time.Duration     `mapstructure:"delay" validate:"omitempty,min=1"`
		Ports   []int             `mapstructure:"ports" validate:"omitempty,min=1,dive,omitempty,min=1"`
		Links   map[string]string `mapstructure:"links" validate:"omitempty,min=1,dive,omitempty,url"`
		URLPtr  *string           `mapstructure:"urlptr" validate:"omitempty,url"`
		PortPtr *int              `mapstructure:"portptr" validate:"omitempty,min=1"`
	}
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"absent", map[string]string{}, ""},
		{"explicit zeros", map[string]string{"URL": "", "PORT": "0", "COUNT": "0", "RATIO": "0", "ENABLED": "false", "DELAY": "0s"}, ""},
		{"valid values", map[string]string{"URL": "https://example.com", "PORT": "80", "COUNT": "2", "RATIO": "1.5", "ENABLED": "true", "DELAY": "1s", "PORTS": "0,80", "LINKS": `{"empty":"","valid":"https://example.com"}`, "URLPTR": "https://example.com", "PORTPTR": "80"}, ""},
		{"bad URL", map[string]string{"URL": "bad"}, "url:"},
		{"whitespace", map[string]string{"URL": "   "}, "url:"},
		{"bad port", map[string]string{"PORT": "-1"}, "port:"},
		{"bad count", map[string]string{"COUNT": "1"}, "count:"},
		{"bad ratio", map[string]string{"RATIO": "0.5"}, "ratio:"},
		{"bad delay", map[string]string{"DELAY": "-1s"}, "delay:"},
		{"empty allocated slice", map[string]string{"PORTS": "[]"}, "ports:"},
		{"bad slice element", map[string]string{"PORTS": "0,-1"}, "ports: [1]:"},
		{"empty allocated map", map[string]string{"LINKS": "{}"}, "links:"},
		{"bad map element", map[string]string{"LINKS": `{"empty":"","bad":"invalid"}`}, "links: [bad]:"},
		{"pointer empty", map[string]string{"URLPTR": ""}, "urlptr:"},
		{"pointer zero", map[string]string{"PORTPTR": "0"}, "portptr:"},
		{"binding error still returned", map[string]string{"PORT": "invalid"}, "port (env PORT):"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := structconf.Load[config](structconf.WithEnvVars(tc.env))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if cfg == nil {
					t.Fatal("nil config on success")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
			if cfg != nil {
				t.Fatal("config returned on validation failure")
			}
		})
	}
}

func TestLoadOmitEmptySources(t *testing.T) {
	type config struct {
		URL string `mapstructure:"url" default:"https://default.example.com" validate:"omitempty,url"`
	}
	cases := []struct {
		name, file, content string
		env                 map[string]string
		want                string
	}{
		{"default", "", "", map[string]string{}, "https://default.example.com"},
		{"empty environment overrides default", "", "", map[string]string{"URL": ""}, ""},
		{"empty yaml", "config.yaml", "url: \"\"\n", map[string]string{}, ""},
		{"empty json", "config.json", `{"url":""}`, map[string]string{}, ""},
		{"empty toml", "config.toml", "url = \"\"\n", map[string]string{}, ""},
		{"empty dotenv", ".env", "URL=\n", map[string]string{}, ""},
		{"environment overrides file", "config.yaml", "url: https://file.example.com\n", map[string]string{"URL": ""}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := []structconf.Option{structconf.WithEnvVars(tc.env)}
			if tc.file != "" {
				path := filepath.Join(t.TempDir(), tc.file)
				if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
				switch tc.file {
				case "config.yaml":
					opts = append(opts, structconf.WithYAMLFile(path))
				case "config.json":
					opts = append(opts, structconf.WithJSONFile(path))
				case "config.toml":
					opts = append(opts, structconf.WithTOMLFile(path))
				case ".env":
					opts = append(opts, structconf.WithDotEnv(path))
				}
			}
			cfg, err := structconf.Load[config](opts...)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.URL != tc.want {
				t.Fatalf("URL=%q, want %q", cfg.URL, tc.want)
			}
		})
	}
}
