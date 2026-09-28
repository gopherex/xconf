package xconf

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	sp "github.com/gopherex/schemapb/go/schemapb"
)

// AllowPaths restricts a source to the given object/map paths and their subtrees.
// Segments are literal keys, not globs or dotted paths. No paths allows nothing;
// an empty Path allows everything. Paths are captured when the wrapper is made.
// Lists must be selected as a whole. Selecting a OneOf discriminator requires
// selecting its whole object, since changing it replaces the previous variant.
// Ancestor edits are projected onto allowed subtrees, preserving their siblings.
// Name, Revision, read errors and watch notifications pass through unchanged.
func AllowPaths(source Source, paths ...Path) Source {
	root := &pathSelection{}
	for _, path := range paths {
		node := root
		for _, key := range path {
			if node.children == nil {
				node.children = map[string]*pathSelection{}
			}
			if node.children[key] == nil {
				node.children[key] = &pathSelection{}
			}
			node = node.children[key]
		}
		node.all = true
	}
	return &allowedPaths{Source: source, selection: root}
}

type allowedPaths struct {
	Source
	selection *pathSelection
}

type pathSelection struct {
	all      bool
	children map[string]*pathSelection
}

func (s *allowedPaths) Watch(ctx context.Context, notify func()) (func(), error) {
	if w, ok := s.Source.(Watcher); ok {
		return w.Watch(ctx, notify)
	}
	return func() {}, nil
}

func (s *allowedPaths) Read(ctx context.Context, schema *sp.Schema) (Layer, error) {
	if err := s.selection.validate(schema, schema, nil, nil); err != nil {
		return Layer{}, err
	}
	layer, err := s.Source.Read(ctx, schema)
	if err != nil {
		return Layer{}, err
	}
	layer, err = cloneLayer(layer)
	if err != nil {
		return Layer{}, err
	}
	values, _ := s.selection.project(layer.Values)
	out := Layer{Values: values.(map[string]any), Revision: layer.Revision, Locations: map[string]Location{}}
	for _, edit := range layer.Edits {
		if (edit.Kind != Replace && edit.Kind != Delete) || len(edit.Path) == 0 {
			return Layer{}, fmt.Errorf("allowed paths: edits require a valid kind and non-root object path")
		}
		if node := s.selection.at(edit.Path); node != nil {
			out.Edits = node.projectEdit(out.Edits, edit)
		}
	}
	for pointer, location := range layer.Locations {
		// Keep ancestor locations too: provenance inherits the nearest location.
		var path Path
		if pointer != "" {
			if !strings.HasPrefix(pointer, "/") {
				continue
			}
			for _, key := range strings.Split(pointer[1:], "/") {
				path = append(path, strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~"))
			}
		}
		if node := s.selection.at(path); node != nil && (node.all || len(node.children) > 0) {
			out.Locations[pointer] = location
		}
	}
	return out, nil
}

// at returns the remaining selection at path; all covers every descendant.
func (s *pathSelection) at(path Path) *pathSelection {
	for _, key := range path {
		if s == nil || s.all {
			return s
		}
		s = s.children[key]
	}
	return s
}

// project omits empty scaffolding and non-object ancestors (including null),
// which must not replace a whole parent when only a child is allowed.
func (s *pathSelection) project(value any) (any, bool) {
	if s.all {
		return value, true
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	out := map[string]any{}
	for key, node := range s.children {
		if value, exists := object[key]; exists {
			if value, keep := node.project(value); keep {
				out[key] = value
			}
		}
	}
	return out, len(out) > 0
}

func (s *pathSelection) projectEdit(out []Edit, edit Edit) []Edit {
	if s.all {
		return append(out, edit)
	}
	object, _ := edit.Value.(map[string]any)
	for _, key := range slices.Sorted(maps.Keys(s.children)) {
		next := Edit{Kind: Delete, Path: child(edit.Path, key)}
		if value, exists := object[key]; edit.Kind == Replace && exists {
			next.Kind, next.Value = Replace, value
		}
		out = s.children[key].projectEdit(out, next)
	}
	return out
}

// Reject selections whose normal merge semantics could overwrite siblings.
// Unknown fields remain subject to the loader's usual schema strictness.
func (s *pathSelection) validate(root, schema *sp.Schema, container *sp.Schema_Field, path Path) error {
	if s.all || len(s.children) == 0 {
		return nil
	}
	if len(path) > 128 {
		return fmt.Errorf("allowed paths: selection exceeds maximum nesting")
	}
	if container.GetList() != nil {
		return fmt.Errorf("allowed paths: select the whole list at %s", path.String())
	}
	if union := container.GetOneOf(); union != nil {
		if s.children[union.GetDiscriminator()] != nil {
			return fmt.Errorf("allowed paths: select the whole OneOf object at %s to include its discriminator", path.String())
		}
		for _, variant := range slices.Sorted(maps.Keys(union.GetVariants())) {
			if err := s.validate(root, union.GetVariants()[variant], nil, path); err != nil {
				return err
			}
		}
		return nil
	}
	for _, key := range slices.Sorted(maps.Keys(s.children)) {
		field := findField(schema, key)
		if m := container.GetMap(); m != nil {
			field = m.GetValueField()
			if field == nil && m.GetValueSchema() != nil {
				field = &sp.Schema_Field{Kind: &sp.Schema_Field_Object_{Object: &sp.Schema_Field_Object{Schema: m.GetValueSchema()}}}
			}
		}
		if err := s.children[key].validate(root, objectSchema(root, field, nil), field, child(path, key)); err != nil {
			return err
		}
	}
	return nil
}
