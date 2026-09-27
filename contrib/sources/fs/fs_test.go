package fs_test

import (
	"context"
	"errors"
	x "github.com/gopherex/xconf"
	json "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/fs"
	"testing"
	"testing/fstest"
)

func TestReadReopensAndOwnsDocument(t *testing.T) {
	data := fstest.MapFS{"config.json": {Data: []byte(`{"a":{"b":1}}`)}}
	s := source.New(data, "config.json", json.Decode)
	layer, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Locations[""].Name != "fs:config.json" {
		t.Fatal(layer.Locations)
	}
	layer.Values["a"].(map[string]any)["b"] = "mutated"
	data["config.json"] = &fstest.MapFile{Data: []byte(`{"a":{"b":2}}`)}
	next, err := s.Read(context.Background(), nil)
	if err != nil || next.Revision == layer.Revision {
		t.Fatal(next, err)
	}
	delete(data, "config.json")
	if _, err = s.Read(context.Background(), nil); !errors.Is(err, x.ErrNotFound) {
		t.Fatal(err)
	}
	for _, s := range []*source.Source{source.New(data, "../bad", json.Decode), source.New(fstest.MapFS{"x": {Data: []byte(`{}`)}}, "x", json.Decode, source.MaxBytes(1))} {
		if _, err = s.Read(context.Background(), nil); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}
