// Package yaml provides a convenience file source and document decoder.
package yaml

import (
	x "github.com/gopherex/xconf"
	decoder "github.com/gopherex/xconf/contrib/decoders/yaml"
	"github.com/gopherex/xconf/contrib/sources/file"
)

func File(path string, opts ...file.Option) *file.Source                { return file.New(path, Decode, opts...) }
func Decode(data []byte) (map[string]any, map[string]x.Location, error) { return decoder.Decode(data) }
