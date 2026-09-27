// Package toml reads TOML documents.
package toml

import (
	"bytes"

	"github.com/BurntSushi/toml"
	x "github.com/gopherex/xconf"
)

func Decode(data []byte) (map[string]any, map[string]x.Location, error) {
	var value map[string]any
	_, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&value)
	if value == nil && err == nil {
		value = map[string]any{}
	}
	return value, nil, err
}
