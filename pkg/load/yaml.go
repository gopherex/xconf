package load

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FromYAMLFile reads path as YAML and returns a MapSource.
func FromYAMLFile(path string) (Source, error) {
	data, err := readYAMLFile(path)
	if err != nil {
		return nil, err
	}
	return FromMap(data), nil
}

// FromYAMLFileOptional behaves like FromYAMLFile but returns an empty
// MapSource (no error) when the file does not exist.
func FromYAMLFileOptional(path string) (Source, error) {
	data, err := readYAMLFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FromMap(nil), nil
		}
		return nil, err
	}
	return FromMap(data), nil
}

func readYAMLFile(path string) (map[string]any, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("xconf/load: %w", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("xconf/load: parse %s: %w", path, err)
	}
	// yaml.v3 decodes nested maps as map[string]any when keys are strings,
	// but mixed-key maps become map[any]any. Normalize.
	return normalizeYAML(data), nil
}

func normalizeYAML(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalizeYAML(t)
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
