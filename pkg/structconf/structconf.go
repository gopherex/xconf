// Package structconf loads configuration into plain Go structs annotated with
// the classic `mapstructure` / `default` / `validate` tag triple, with no
// dependency on viper.
//
// It is a drop-in replacement for the common go-server-toolkit configurator:
// the same structs that used to be loaded by viper + creasty/defaults +
// go-playground/validator load here unchanged. The only difference is the
// engine — env naming, defaults and validation are all implemented on top of
// xconf primitives (notably pkg/validate), so there are no external runtime
// deps beyond the YAML parser already used by xconf.
//
//	type Postgres struct {
//	    Host string `mapstructure:"host" default:"localhost" validate:"required,hostname|ip"`
//	    Port int    `mapstructure:"port" default:"5432"      validate:"required,min=1,max=65535"`
//	}
//	type Config struct {
//	    Infra struct {
//	        Postgres Postgres `mapstructure:"postgres"`
//	    } `mapstructure:"infra"`
//	}
//
//	// env-only (no file):  INFRA_POSTGRES_HOST, INFRA_POSTGRES_PORT, ...
//	cfg, err := structconf.Load[Config]()
//
//	// file + env (env wins):
//	cfg, err := structconf.Load[Config](structconf.WithYAMLFile("config.yaml"))
//
// Precedence per field: tag default < YAML file value < environment variable.
//
// Env var names are derived from the dotted mapstructure path, upper-cased with
// dots replaced by underscores (infra.postgres.host -> INFRA_POSTGRES_HOST),
// matching the legacy configurator convention. An optional prefix can be added
// via WithEnvPrefix.
package structconf

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

// Option configures Load.
type Option func(*options)

type fileSpec struct {
	path     string
	format   string // "yaml" | "json" | "toml"
	optional bool
}

type options struct {
	files       []fileSpec
	envPrefix   string            // prepended to every derived env name (no trailing _)
	envVars     map[string]string // when non-nil, used instead of the process env (tests)
	dotenvPaths []fileSpec        // .env files (optional flag honored)
	dotenv      map[string]string // loaded .env values (fallback under real env)
}

// WithYAMLFile loads from a YAML file. A missing file is an error; use the
// Optional variant to ignore absence.
func WithYAMLFile(path string) Option { return addFile(path, "yaml", false) }

// WithYAMLFileOptional is WithYAMLFile but skips a missing file.
func WithYAMLFileOptional(path string) Option { return addFile(path, "yaml", true) }

// WithJSONFile loads from a JSON file.
func WithJSONFile(path string) Option { return addFile(path, "json", false) }

// WithJSONFileOptional is WithJSONFile but skips a missing file.
func WithJSONFileOptional(path string) Option { return addFile(path, "json", true) }

// WithTOMLFile loads from a TOML file.
func WithTOMLFile(path string) Option { return addFile(path, "toml", false) }

// WithTOMLFileOptional is WithTOMLFile but skips a missing file.
func WithTOMLFileOptional(path string) Option { return addFile(path, "toml", true) }

// WithFile loads a file, detecting the format from its extension
// (.yaml/.yml/.json/.toml).
func WithFile(path string) Option { return addFile(path, formatFromExt(path), false) }

// WithFileOptional is WithFile but skips a missing file.
func WithFileOptional(path string) Option { return addFile(path, formatFromExt(path), true) }

func addFile(path, format string, optional bool) Option {
	return func(o *options) {
		o.files = append(o.files, fileSpec{path: path, format: format, optional: optional})
	}
}

// WithConfigPath replicates the legacy configurator's discovery: it looks for
// <name>.{yaml,yml,json,toml} inside the directory named by the CONFIG_PATH env
// var (default "."), and loads the first that exists. Missing file is not an
// error. name defaults to "config" when empty.
//
// CONFIG_PATH may also point directly at a file, in which case that file is
// used and name/extension search is skipped.
func WithConfigPath(name string) Option {
	return func(o *options) {
		if spec, ok := resolveConfigPath(name); ok {
			o.files = append(o.files, spec)
		}
	}
}

// WithDotEnv loads KEY=VALUE pairs from a .env file into the environment layer
// as a fallback: real process env vars still take precedence. Missing file is
// not an error. With no path, ".env" in the working directory is used.
func WithDotEnv(path ...string) Option {
	return func(o *options) {
		p := ".env"
		if len(path) > 0 && path[0] != "" {
			p = path[0]
		}
		o.dotenvPaths = append(o.dotenvPaths, fileSpec{path: p, format: "dotenv", optional: true})
	}
}

// WithEnvPrefix prefixes every derived env name, e.g. WithEnvPrefix("IAM")
// makes infra.postgres.host resolve from IAM_INFRA_POSTGRES_HOST.
func WithEnvPrefix(prefix string) Option {
	return func(o *options) { o.envPrefix = strings.Trim(strings.ToUpper(prefix), "_") }
}

// WithEnvVars overrides the process environment with an explicit map. Useful in
// tests. Passing nil (the default) reads from os.LookupEnv.
func WithEnvVars(vars map[string]string) Option {
	return func(o *options) { o.envVars = vars }
}

// Load builds a *T, applying tag defaults, then config files (in option order,
// later overriding earlier), then environment variables (.env as fallback under
// real env), and finally runs `validate` tags.
//
// Per field precedence: default < files < .env < environment.
func Load[T any](opts ...Option) (*T, error) {
	o := &options{}
	for _, fn := range opts {
		fn(o)
	}

	cfg := new(T)
	rv := reflect.ValueOf(cfg).Elem()
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("structconf: T must be a struct, got %v", rv.Kind())
	}

	if err := o.loadDotEnv(); err != nil {
		return nil, err
	}
	fileMap, err := o.loadFiles()
	if err != nil {
		return nil, err
	}

	if err := bindStruct(rv, nil, fileMap, o); err != nil {
		return nil, err
	}
	if err := validateStruct(rv, nil); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadFiles reads and deep-merges every configured file in order.
func (o *options) loadFiles() (map[string]any, error) {
	var acc map[string]any
	for _, f := range o.files {
		m, err := parseFile(f)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		if acc == nil {
			acc = m
		} else {
			acc = mergeMaps(acc, m)
		}
	}
	return acc, nil
}

func (o *options) loadDotEnv() error {
	for _, f := range o.dotenvPaths {
		m, err := parseDotEnv(f)
		if err != nil {
			return err
		}
		if o.dotenv == nil {
			o.dotenv = map[string]string{}
		}
		for k, v := range m {
			o.dotenv[k] = v
		}
	}
	return nil
}

func (o *options) lookupEnv(name string) (string, bool) {
	if o.envPrefix != "" {
		name = o.envPrefix + "_" + name
	}
	// Real env (or test override) wins; .env is a fallback.
	if o.envVars != nil {
		if v, ok := o.envVars[name]; ok {
			return v, true
		}
	} else if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	if v, ok := o.dotenv[name]; ok {
		return v, true
	}
	return "", false
}

// bindStruct walks rv (a struct value) honoring mapstructure tags. path is the
// dotted mapstructure path accumulated so far; fileMap is the YAML submap at
// this level (may be nil).
func bindStruct(rv reflect.Value, path []string, fileMap map[string]any, o *options) error {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		ms, ok := parseMapstructure(sf)
		if !ok {
			continue // not part of config (matches legacy)
		}
		fv := rv.Field(i)

		ft := sf.Type
		isPtr := ft.Kind() == reflect.Pointer
		elemT := ft
		if isPtr {
			elemT = ft.Elem()
		}

		// Nested struct (but not time.Time) => recurse. A ,squash field keeps
		// the current path and fileMap level (fields are flattened).
		if elemT.Kind() == reflect.Struct && elemT != timeType {
			if isPtr && fv.IsNil() {
				fv.Set(reflect.New(elemT))
			}
			if isPtr {
				fv = fv.Elem()
			}
			cpath := path
			sub := fileMap
			if !ms.squash {
				cpath = append(append([]string(nil), path...), ms.name)
				sub, _ = fileMap[ms.name].(map[string]any)
			}
			if err := bindStruct(fv, cpath, sub, o); err != nil {
				return err
			}
			continue
		}

		// Leaf. Resolve value: default < file < env.
		fpath := append(append([]string(nil), path...), ms.name)
		var (
			raw   any
			found bool
		)
		if d, has := sf.Tag.Lookup("default"); has {
			raw, found = d, true
		}
		if fileMap != nil {
			if v, ok := fileMap[ms.name]; ok {
				raw, found = v, true
			}
		}
		envName := strings.ToUpper(strings.Join(fpath, "_"))
		if v, ok := o.lookupEnv(envName); ok {
			raw, found = v, true
		}
		if !found {
			continue // leave zero value; pointer stays nil
		}
		target := fv
		if isPtr {
			fv.Set(reflect.New(elemT))
			target = fv.Elem()
		}
		if err := setValue(target, raw); err != nil {
			return fmt.Errorf("%s (env %s): %w", strings.Join(fpath, "."), envName, err)
		}
	}
	return nil
}

type msTag struct {
	name   string
	squash bool
}

// parseMapstructure returns the field's mapstructure config and whether it
// participates. A `-` or empty named tag opts out. Anonymous embedded structs
// and the `,squash` modifier flatten their fields into the parent level.
func parseMapstructure(sf reflect.StructField) (msTag, bool) {
	tag, has := sf.Tag.Lookup("mapstructure")
	if !has {
		// Anonymous embedded struct without a tag is squashed (mapstructure
		// and the legacy configurator both treat it as part of the parent).
		if sf.Anonymous {
			return msTag{squash: true}, true
		}
		return msTag{}, false
	}
	parts := strings.Split(tag, ",")
	name := strings.TrimSpace(parts[0])
	squash := false
	for _, m := range parts[1:] {
		if strings.TrimSpace(m) == "squash" {
			squash = true
		}
	}
	if squash || (name == "" && sf.Anonymous) {
		return msTag{squash: true}, true
	}
	if name == "" || name == "-" {
		return msTag{}, false
	}
	return msTag{name: name}, true
}

// normalize coerces yaml.v3's map[any]any nodes into map[string]any so lookups
// by string key work at every depth.
func normalize(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalize(t)
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprintf("%v", k)] = normalizeValue(val)
		}
		return out
	case []any:
		for i, x := range t {
			t[i] = normalizeValue(x)
		}
		return t
	}
	return v
}
