package document_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
)

func TestReadBoundsAndCancellation(t *testing.T) {
	for _, limit := range []int64{0, -1, math.MaxInt64, 2} {
		if _, err := document.Read(context.Background(), bytes.NewBufferString("abc"), limit); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := document.Read(ctx, bytes.NewBufferString("abc"), 3); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data, err := document.Read(context.Background(), bytes.NewBufferString("abc"), 3)
	if err != nil || string(data) != "abc" {
		t.Fatal(string(data), err)
	}
}

func TestDecoderCannotMutateCachedBytesOrLocations(t *testing.T) {
	data := []byte("input")
	locations := map[string]x.Location{"/n": {Line: 2}}
	decode := func(b []byte) (map[string]any, map[string]x.Location, error) {
		b[0] = '!'
		return map[string]any{"n": 1}, locations, nil
	}
	layer, err := document.Decode(data, decode, "document", "")
	if err != nil || string(data) != "input" || locations["/n"].Name != "" {
		t.Fatal(layer, err, string(data), locations)
	}
	if layer.Locations["/n"].Name != "document" || layer.Locations[""].Name != "document" {
		t.Fatal(layer)
	}
}
