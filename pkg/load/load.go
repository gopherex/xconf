// Package load is the runtime, reflection-based loader for xconf schemas.
//
// Sources are queried in order; later sources override earlier ones. Defaults
// declared on the schema win over nothing but lose to any source hit.
//
//	cfg := &AppConfig{}
//	err := load.Load(app.Schema, cfg,
//	    load.FromMap(map[string]any{"Port": 9090}),
//	    load.FromEnv(nil), // env overrides map
//	)
package load

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/gopherex/xconf"
)

// Source is a value provider for a single field.
//
// path is the dotted field path from the schema root (e.g.
// ["Redis", "Addr"]). Sources may key off path, d.Env, or both.
type Source interface {
	Lookup(d xconf.FieldDesc, path []string) (raw any, ok bool, err error)
}

// Load walks schema and fills target (must be a non-nil pointer to a struct).
// Per-field resolution order: declared Default < each Source in argument
// order (later wins). Validators run after the final value is set.
func Load(schema *xconf.Schema, target any, sources ...Source) error {
	if schema == nil {
		return fmt.Errorf("xconf/load: nil schema")
	}
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return fmt.Errorf("xconf/load: target must be non-nil pointer, got %T", target)
	}
	tv = tv.Elem()
	if tv.Kind() != reflect.Struct {
		return fmt.Errorf("xconf/load: target must point to struct, got %v", tv.Kind())
	}
	return walkGroup(schema.Describe(), tv, nil, sources)
}

func walkGroup(g xconf.FieldDesc, tv reflect.Value, path []string, sources []Source) error {
	for _, child := range g.Children {
		cpath := append(slices.Clone(path), child.Name)
		fv := tv.FieldByName(child.Name)
		if !fv.IsValid() {
			return fmt.Errorf("%s: target struct has no field %q", strings.Join(cpath, "."), child.Name)
		}
		if !fv.CanSet() {
			return fmt.Errorf("%s: field is not settable", strings.Join(cpath, "."))
		}

		if child.Kind == xconf.KindGroup {
			if fv.Kind() != reflect.Struct {
				return fmt.Errorf("%s: schema expects struct, target has %v", strings.Join(cpath, "."), fv.Kind())
			}
			// Delegate to bound loader, if any. Loader result is authoritative
			// for the whole subtree; we do not walk children afterwards.
			if child.LoaderFn != nil {
				if err := child.LoaderFn(fv); err != nil {
					return fmt.Errorf("%s: %w", strings.Join(cpath, "."), err)
				}
				continue
			}
			if err := walkGroup(child, fv, cpath, sources); err != nil {
				return err
			}
			continue
		}

		raw, found, err := resolveLeaf(child, cpath, sources)
		if err != nil {
			return fmt.Errorf("%s: %w", strings.Join(cpath, "."), err)
		}
		if !found {
			if child.Required {
				return fmt.Errorf("%s: required, no value provided", strings.Join(cpath, "."))
			}
			continue
		}
		if err := setField(child, raw, fv); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(cpath, "."), err)
		}
		for _, vfn := range child.Validators {
			if err := vfn(fv.Interface()); err != nil {
				return fmt.Errorf("%s: %w", strings.Join(cpath, "."), err)
			}
		}
	}
	return nil
}

func resolveLeaf(d xconf.FieldDesc, path []string, sources []Source) (any, bool, error) {
	var raw any
	found := false
	if d.HasDefault {
		raw = d.Default
		found = true
	}
	for _, src := range sources {
		v, ok, err := src.Lookup(d, path)
		if err != nil {
			return nil, false, err
		}
		if ok {
			raw = v
			found = true
		}
	}
	return raw, found, nil
}
