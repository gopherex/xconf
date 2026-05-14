package load

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FromTOMLFile reads path as TOML and returns a MapSource.
func FromTOMLFile(path string) (Source, error) {
	data, err := readTOMLFile(path)
	if err != nil {
		return nil, err
	}
	return FromMap(data), nil
}

// FromTOMLFileOptional behaves like FromTOMLFile but returns an empty
// MapSource (no error) when the file does not exist.
func FromTOMLFileOptional(path string) (Source, error) {
	data, err := readTOMLFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FromMap(nil), nil
		}
		return nil, err
	}
	return FromMap(data), nil
}

func readTOMLFile(path string) (map[string]any, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("xconf/load: %w", err)
	}
	var data map[string]any
	if _, err := toml.DecodeFile(abs, &data); err != nil {
		// distinguish missing file
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("xconf/load: parse %s: %w", path, err)
	}
	return data, nil
}
