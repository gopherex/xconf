// Package document contains shared bounded document reading for source adapters.
package document

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"

	x "github.com/gopherex/xconf"
)

const DefaultMaxBytes int64 = 8 << 20

func Read(ctx context.Context, r io.Reader, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit == math.MaxInt64 {
		return nil, errors.New("invalid document size limit")
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("document exceeds size limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// Decode attaches a fallback location and content hash without modifying the input.
func Decode(data []byte, decoder x.Decoder, name, revision string) (x.Layer, error) {
	if decoder == nil {
		return x.Layer{}, errors.New("nil document decoder")
	}
	values, locations, err := decoder(bytes.Clone(data))
	if err != nil {
		return x.Layer{}, err
	}
	if values == nil {
		return x.Layer{}, errors.New("document must be an object")
	}
	loc := make(map[string]x.Location, len(locations)+1)
	loc[""] = x.Location{Name: name}
	for p, l := range locations {
		if l.Name == "" {
			l.Name = name
		}
		loc[p] = l
	}
	if revision == "" {
		revision = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return x.Layer{Values: values, Locations: loc, Revision: revision}, nil
}
