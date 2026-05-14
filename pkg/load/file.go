package load

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FromJSONFile reads path as JSON and returns a MapSource.
//
// Missing files return an error; callers that want soft-optional config
// should check existence themselves or use FromJSONFileOptional.
func FromJSONFile(path string) (Source, error) {
	data, err := readJSONFile(path)
	if err != nil {
		return nil, err
	}
	return FromMap(data), nil
}

// FromJSONFileOptional behaves like FromJSONFile but returns an empty
// MapSource (no error) when the file does not exist.
func FromJSONFileOptional(path string) (Source, error) {
	data, err := readJSONFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FromMap(nil), nil
		}
		return nil, err
	}
	return FromMap(data), nil
}

func readJSONFile(path string) (map[string]any, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("xconf/load: %w", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("xconf/load: parse %s: %w", path, err)
	}
	return data, nil
}
