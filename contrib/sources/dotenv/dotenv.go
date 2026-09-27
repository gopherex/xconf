// Package dotenv reads .env files without changing the process environment.
package dotenv

import (
	"context"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/env"
	"github.com/gopherex/xconf/contrib/sources/file"
	"github.com/joho/godotenv"
)

type Source struct {
	path    string
	options []env.Option
}

func File(path string, opts ...env.Option) *Source {
	return &Source{path, append([]env.Option(nil), opts...)}
}
func (s *Source) Name() string { return s.path }
func (s *Source) Read(ctx context.Context, schema *sp.Schema) (x.Layer, error) {
	return file.New(s.path, func(data []byte) (map[string]any, map[string]x.Location, error) {
		vars, err := godotenv.Unmarshal(string(data))
		if err != nil {
			return nil, nil, err
		}
		layer, err := env.Decode(schema, vars, s.options...)
		for key, loc := range layer.Locations {
			loc.Name = s.path + ":" + loc.Name
			layer.Locations[key] = loc
		}
		return layer.Values, layer.Locations, err
	}).Read(ctx, schema)
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	return x.Poll(s, 250*time.Millisecond).(x.Watcher).Watch(ctx, notify)
}
