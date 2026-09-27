// Package json reads JSON object documents without rounding integer tokens.
package json

import (
	"bytes"
	std "encoding/json"
	"errors"
	"io"

	x "github.com/gopherex/xconf"
)

func Decode(data []byte) (map[string]any, map[string]x.Location, error) {
	d := std.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value map[string]any
	if err := d.Decode(&value); err != nil {
		return nil, nil, err
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return nil, nil, errors.New("trailing JSON content")
	}
	if value == nil {
		return nil, nil, errors.New("JSON root must be an object")
	}
	return value, nil, nil
}
