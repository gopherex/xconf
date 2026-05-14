// Package validate provides typed validators for xconf fields.
//
// Each validator returns an xconf.Validator[T] that can be attached to a
// field via .Validate(...). Validators are typed: the generic parameter is
// inferred from usage so Range[int](0, 65535) is a Validator[int] and
// rejects misuse at compile time.
//
//	import (
//	    "github.com/gopherex/xconf"
//	    "github.com/gopherex/xconf/pkg/validate"
//	)
//
//	xconf.Int("Port").Validate(validate.Range(1, 65535))
//	xconf.String("Mode").Validate(validate.OneOf("dev", "prod"))
//	xconf.Slice[string]("Hosts").Validate(validate.MinItems[string](1))
package validate

import (
	"cmp"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/gopherex/xconf"
)

// ---------------------------------------------------------------------------
// Generic / collection
// ---------------------------------------------------------------------------

// All runs every validator and returns the first error.
func All[T any](vs ...xconf.Validator[T]) xconf.Validator[T] {
	return func(v T) error {
		for _, fn := range vs {
			if err := fn(v); err != nil {
				return err
			}
		}
		return nil
	}
}

// Any passes if at least one validator passes. Returns a combined error
// listing each branch's failure if none pass.
func Any[T any](vs ...xconf.Validator[T]) xconf.Validator[T] {
	return func(v T) error {
		var errs []string
		for _, fn := range vs {
			if err := fn(v); err == nil {
				return nil
			} else {
				errs = append(errs, err.Error())
			}
		}
		return fmt.Errorf("no branch matched: %s", strings.Join(errs, "; "))
	}
}

// Not inverts a validator. If inner passes, Not fails with msg.
func Not[T any](inner xconf.Validator[T], msg string) xconf.Validator[T] {
	return func(v T) error {
		if inner(v) == nil {
			if msg == "" {
				msg = "value matched forbidden condition"
			}
			return fmt.Errorf("%s", msg)
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// Numeric / ordered
// ---------------------------------------------------------------------------

// Range ensures min <= v <= max.
func Range[T cmp.Ordered](min, max T) xconf.Validator[T] {
	return func(v T) error {
		if v < min || v > max {
			return fmt.Errorf("value %v out of range [%v, %v]", v, min, max)
		}
		return nil
	}
}

// Min ensures v >= min.
func Min[T cmp.Ordered](min T) xconf.Validator[T] {
	return func(v T) error {
		if v < min {
			return fmt.Errorf("value %v less than min %v", v, min)
		}
		return nil
	}
}

// Max ensures v <= max.
func Max[T cmp.Ordered](max T) xconf.Validator[T] {
	return func(v T) error {
		if v > max {
			return fmt.Errorf("value %v greater than max %v", v, max)
		}
		return nil
	}
}

// Positive ensures v > 0.
func Positive[T numeric]() xconf.Validator[T] {
	return func(v T) error {
		if v <= 0 {
			return fmt.Errorf("value %v must be positive", v)
		}
		return nil
	}
}

// NonNegative ensures v >= 0.
func NonNegative[T numeric]() xconf.Validator[T] {
	return func(v T) error {
		if v < 0 {
			return fmt.Errorf("value %v must be non-negative", v)
		}
		return nil
	}
}

// NonZero ensures v != 0.
func NonZero[T numeric]() xconf.Validator[T] {
	return func(v T) error {
		if v == 0 {
			return fmt.Errorf("value must not be zero")
		}
		return nil
	}
}

type numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// ---------------------------------------------------------------------------
// Equality / sets
// ---------------------------------------------------------------------------

// OneOf ensures v is one of the allowed values.
func OneOf[T comparable](allowed ...T) xconf.Validator[T] {
	return func(v T) error {
		if slices.Contains(allowed, v) {
			return nil
		}
		return fmt.Errorf("value %v not in allowed set %v", v, allowed)
	}
}

// Equal ensures v == want.
func Equal[T comparable](want T) xconf.Validator[T] {
	return func(v T) error {
		if v != want {
			return fmt.Errorf("value %v != %v", v, want)
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// String
// ---------------------------------------------------------------------------

// NonEmpty rejects empty strings.
func NonEmpty() xconf.Validator[string] {
	return func(v string) error {
		if v == "" {
			return fmt.Errorf("value must not be empty")
		}
		return nil
	}
}

// MinLen ensures len(v) >= n.
func MinLen(n int) xconf.Validator[string] {
	return func(v string) error {
		if len(v) < n {
			return fmt.Errorf("length %d < min %d", len(v), n)
		}
		return nil
	}
}

// MaxLen ensures len(v) <= n.
func MaxLen(n int) xconf.Validator[string] {
	return func(v string) error {
		if len(v) > n {
			return fmt.Errorf("length %d > max %d", len(v), n)
		}
		return nil
	}
}

// LenBetween ensures min <= len(v) <= max.
func LenBetween(min, max int) xconf.Validator[string] {
	return All(MinLen(min), MaxLen(max))
}

// Regex ensures v matches the compiled pattern.
func Regex(pattern string) xconf.Validator[string] {
	re := regexp.MustCompile(pattern)
	return func(v string) error {
		if !re.MatchString(v) {
			return fmt.Errorf("value %q does not match %s", v, pattern)
		}
		return nil
	}
}

// HasPrefix ensures v starts with prefix.
func HasPrefix(prefix string) xconf.Validator[string] {
	return func(v string) error {
		if !strings.HasPrefix(v, prefix) {
			return fmt.Errorf("value %q must start with %q", v, prefix)
		}
		return nil
	}
}

// HasSuffix ensures v ends with suffix.
func HasSuffix(suffix string) xconf.Validator[string] {
	return func(v string) error {
		if !strings.HasSuffix(v, suffix) {
			return fmt.Errorf("value %q must end with %q", v, suffix)
		}
		return nil
	}
}

// Contains ensures v contains sub.
func Contains(sub string) xconf.Validator[string] {
	return func(v string) error {
		if !strings.Contains(v, sub) {
			return fmt.Errorf("value %q must contain %q", v, sub)
		}
		return nil
	}
}

// URL ensures v parses as an absolute URL.
func URL() xconf.Validator[string] {
	return func(v string) error {
		u, err := url.Parse(v)
		if err != nil {
			return fmt.Errorf("invalid URL %q: %w", v, err)
		}
		if !u.IsAbs() {
			return fmt.Errorf("URL %q must be absolute", v)
		}
		return nil
	}
}

var emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Email ensures v looks like an email address.
func Email() xconf.Validator[string] {
	return func(v string) error {
		if !emailRE.MatchString(v) {
			return fmt.Errorf("invalid email %q", v)
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// Slice
// ---------------------------------------------------------------------------

// MinItems ensures len(slice) >= n.
func MinItems[T any](n int) xconf.Validator[[]T] {
	return func(v []T) error {
		if len(v) < n {
			return fmt.Errorf("items %d < min %d", len(v), n)
		}
		return nil
	}
}

// MaxItems ensures len(slice) <= n.
func MaxItems[T any](n int) xconf.Validator[[]T] {
	return func(v []T) error {
		if len(v) > n {
			return fmt.Errorf("items %d > max %d", len(v), n)
		}
		return nil
	}
}

// Unique ensures all elements are distinct.
func Unique[T comparable]() xconf.Validator[[]T] {
	return func(v []T) error {
		seen := make(map[T]struct{}, len(v))
		for _, x := range v {
			if _, ok := seen[x]; ok {
				return fmt.Errorf("duplicate value %v", x)
			}
			seen[x] = struct{}{}
		}
		return nil
	}
}

// Each applies inner to every element.
func Each[T any](inner xconf.Validator[T]) xconf.Validator[[]T] {
	return func(v []T) error {
		for i, x := range v {
			if err := inner(x); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// Map
// ---------------------------------------------------------------------------

// MapMinSize ensures len(m) >= n.
func MapMinSize[K comparable, V any](n int) xconf.Validator[map[K]V] {
	return func(v map[K]V) error {
		if len(v) < n {
			return fmt.Errorf("size %d < min %d", len(v), n)
		}
		return nil
	}
}

// MapMaxSize ensures len(m) <= n.
func MapMaxSize[K comparable, V any](n int) xconf.Validator[map[K]V] {
	return func(v map[K]V) error {
		if len(v) > n {
			return fmt.Errorf("size %d > max %d", len(v), n)
		}
		return nil
	}
}

// MapHasKey ensures all required keys are present.
func MapHasKey[K comparable, V any](keys ...K) xconf.Validator[map[K]V] {
	return func(v map[K]V) error {
		for _, k := range keys {
			if _, ok := v[k]; !ok {
				return fmt.Errorf("missing required key %v", k)
			}
		}
		return nil
	}
}

// MapKeys applies inner to every key.
func MapKeys[K comparable, V any](inner xconf.Validator[K]) xconf.Validator[map[K]V] {
	return func(v map[K]V) error {
		for k := range v {
			if err := inner(k); err != nil {
				return fmt.Errorf("key %v: %w", k, err)
			}
		}
		return nil
	}
}

// MapValues applies inner to every value.
func MapValues[K comparable, V any](inner xconf.Validator[V]) xconf.Validator[map[K]V] {
	return func(v map[K]V) error {
		for k, val := range v {
			if err := inner(val); err != nil {
				return fmt.Errorf("key %v: %w", k, err)
			}
		}
		return nil
	}
}
