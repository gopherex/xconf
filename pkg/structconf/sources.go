package structconf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// formatFromExt maps a file extension to a parser name. Unknown extensions
// default to YAML.
func formatFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	default:
		return "yaml"
	}
}

// parseFile reads and decodes a config file into a normalized map. A missing
// optional file yields (nil, nil).
func parseFile(f fileSpec) (map[string]any, error) {
	abs, err := filepath.Abs(f.path)
	if err != nil {
		return nil, fmt.Errorf("structconf: %w", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		if f.optional && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("structconf: read %s: %w", f.path, err)
	}

	var data map[string]any
	switch f.format {
	case "json":
		if err := json.Unmarshal(b, &data); err != nil {
			return nil, fmt.Errorf("structconf: parse %s: %w", f.path, err)
		}
	case "toml":
		if err := toml.Unmarshal(b, &data); err != nil {
			return nil, fmt.Errorf("structconf: parse %s: %w", f.path, err)
		}
	default: // yaml
		if err := yaml.Unmarshal(b, &data); err != nil {
			return nil, fmt.Errorf("structconf: parse %s: %w", f.path, err)
		}
	}
	return normalize(data), nil
}

// resolveConfigPath implements the legacy CONFIG_PATH discovery. It returns the
// first existing <dir>/<name>.<ext> (ext in yaml,yml,json,toml), or treats
// CONFIG_PATH as a direct file path when it names a file.
func resolveConfigPath(name string) (fileSpec, bool) {
	if name == "" {
		name = "config"
	}
	dir := os.Getenv("CONFIG_PATH")
	if dir == "" {
		dir = "."
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return fileSpec{path: dir, format: formatFromExt(dir), optional: true}, true
	}
	for _, ext := range []string{"yaml", "yml", "json", "toml"} {
		p := filepath.Join(dir, name+"."+ext)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return fileSpec{path: p, format: formatFromExt(p), optional: true}, true
		}
	}
	return fileSpec{}, false
}

// parseDotEnv reads a .env file into a flat map. Lines may be blank, comments
// (#...), or KEY=VALUE (with optional leading `export`). Surrounding single or
// double quotes on the value are stripped. A missing optional file yields nil.
func parseDotEnv(f fileSpec) (map[string]string, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if f.optional && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("structconf: read %s: %w", f.path, err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		if k != "" {
			out[k] = v
		}
	}
	return out, nil
}

// mergeMaps deep-merges src over a clone of dst (src wins on scalar conflicts;
// nested maps recurse).
func mergeMaps(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst)+len(src))
	for k, v := range dst {
		out[k] = v
	}
	for k, sv := range src {
		if dv, ok := out[k]; ok {
			if dm, ok1 := dv.(map[string]any); ok1 {
				if sm, ok2 := sv.(map[string]any); ok2 {
					out[k] = mergeMaps(dm, sm)
					continue
				}
			}
		}
		out[k] = sv
	}
	return out
}
