package load

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gopherex/xconf"
)

// Validate walks schema and runs each leaf's validators against the
// corresponding field of target. Use this after LoadFromEnv (which skips
// validators on the hot path) when validation is desired.
//
// target must be a non-nil pointer to the struct produced by codegen.
func Validate(schema *xconf.Schema, target any) error {
	if schema == nil {
		return fmt.Errorf("xconf/load: nil schema")
	}
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return fmt.Errorf("xconf/load: Validate target must be non-nil pointer, got %T", target)
	}
	return validateGroup(schema.Describe(), tv.Elem(), nil)
}

func validateGroup(g xconf.FieldDesc, tv reflect.Value, path []string) error {
	for _, child := range g.Children {
		cpath := append(append([]string(nil), path...), child.Name)
		fv := tv.FieldByName(child.Name)
		if !fv.IsValid() {
			return fmt.Errorf("%s: target has no field %q", strings.Join(cpath, "."), child.Name)
		}
		if child.Kind == xconf.KindGroup {
			// A bound-loader group is opaque from a validation standpoint;
			// trust the loader's own checks.
			if child.LoaderFn != nil {
				continue
			}
			if err := validateGroup(child, fv, cpath); err != nil {
				return err
			}
			continue
		}
		for _, vfn := range child.Validators {
			if err := vfn(fv.Interface()); err != nil {
				return fmt.Errorf("%s: %w", strings.Join(cpath, "."), err)
			}
		}
	}
	return nil
}
