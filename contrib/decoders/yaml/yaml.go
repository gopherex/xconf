// Package yaml reads one YAML mapping, preserving exact integer tokens.
package yaml

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	x "github.com/gopherex/xconf"
	"gopkg.in/yaml.v3"
)

func Decode(data []byte) (map[string]any, map[string]x.Location, error) {
	d := yaml.NewDecoder(bytes.NewReader(data))
	var node yaml.Node
	if err := d.Decode(&node); err != nil {
		return nil, nil, err
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		return nil, nil, errors.New("expected one YAML document")
	}
	if len(node.Content) != 1 {
		return nil, nil, errors.New("empty YAML document")
	}
	loc := map[string]x.Location{}
	budget := 100000
	value, err := walk(node.Content[0], nil, loc, 0, &budget)
	if err != nil {
		return nil, nil, err
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, nil, errors.New("YAML root must be an object")
	}
	return root, loc, nil
}
func walk(n *yaml.Node, p x.Path, loc map[string]x.Location, depth int, budget *int) (any, error) {
	*budget--
	if *budget < 0 {
		return nil, errors.New("YAML node limit")
	}
	if depth > 128 {
		return nil, errors.New("YAML nesting limit")
	}
	loc[p.String()] = x.Location{Line: n.Line, Column: n.Column}
	child := func(k string) x.Path { return append(append(x.Path(nil), p...), k) }
	switch n.Kind {
	case yaml.MappingNode:
		out := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Tag != "!!str" {
				return nil, errors.New("YAML keys must be strings; merge keys are unsupported")
			}
			if _, exists := out[k.Value]; exists {
				return nil, errors.New("duplicate YAML key")
			}
			v, err := walk(n.Content[i+1], child(k.Value), loc, depth+1, budget)
			if err != nil {
				return nil, err
			}
			out[k.Value] = v
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		for i, node := range n.Content {
			v, err := walk(node, child(strconv.Itoa(i)), loc, depth+1, budget)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case yaml.AliasNode:
		return walk(n.Alias, p, loc, depth+1, budget)
	case yaml.ScalarNode:
		if n.Tag == "!!float" {
			raw := strings.ReplaceAll(n.Value, "_", "")
			if strings.HasPrefix(raw, ".") {
				raw = "0" + raw
			}
			if strings.HasPrefix(raw, "-.") {
				raw = "-0" + raw[1:]
			}
			if strings.HasPrefix(raw, "+.") {
				raw = "0" + raw[1:]
			}
			raw = strings.TrimPrefix(raw, "+")
			if strings.HasSuffix(raw, ".") {
				raw += "0"
			}
			if !json.Valid([]byte(raw)) {
				return nil, errors.New("non-finite or invalid YAML number")
			}
			return json.Number(raw), nil
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		switch v.(type) {
		case nil, string, bool, int, int64, uint64, time.Time:
			return v, nil
		default:
			return nil, errors.New("unsupported YAML scalar")
		}
	}
	return nil, errors.New("unsupported YAML node")
}
