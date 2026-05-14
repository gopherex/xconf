package core

import (
	"errors"
	"time"
)

var errValidatorType = errors.New("xconf: validator type mismatch")

// Field is a typed configuration leaf. Chain methods return *Field[T] so the
// generic parameter T is preserved through the fluent API.
type Field[T any] struct {
	name        string
	kind        Kind
	goType      string
	def         *T
	env         string
	required    bool
	description string
	validators  []Validator[T]
}

func newField[T any](name string, kind Kind, goType string) *Field[T] {
	return &Field[T]{name: name, kind: kind, goType: goType}
}

func (f *Field[T]) Default(v T) *Field[T]          { f.def = &v; return f }
func (f *Field[T]) Env(name string) *Field[T]      { f.env = name; return f }
func (f *Field[T]) Required() *Field[T]            { f.required = true; return f }
func (f *Field[T]) Description(s string) *Field[T] { f.description = s; return f }
func (f *Field[T]) Validate(v Validator[T]) *Field[T] {
	f.validators = append(f.validators, v)
	return f
}

// Validators returns the typed validators. Used by runtime binders.
func (f *Field[T]) Validators() []Validator[T] { return f.validators }

func (f *Field[T]) describe() FieldDesc {
	d := FieldDesc{
		Name:        f.name,
		Kind:        f.kind,
		GoType:      f.goType,
		Env:         f.env,
		Required:    f.required,
		Description: f.description,
		Validators:  eraseValidators(f.validators),
	}
	if f.def != nil {
		d.Default = *f.def
		d.HasDefault = true
	}
	return d
}

func eraseValidators[T any](vs []Validator[T]) []func(any) error {
	if len(vs) == 0 {
		return nil
	}
	out := make([]func(any) error, len(vs))
	for i, fn := range vs {
		fn := fn
		out[i] = func(v any) error {
			t, ok := v.(T)
			if !ok {
				return errValidatorType
			}
			return fn(t)
		}
	}
	return out
}

// Typed constructors.
func Int(name string) *Field[int]         { return newField[int](name, KindInt, "int") }
func Int8(name string) *Field[int8]       { return newField[int8](name, KindInt8, "int8") }
func Int16(name string) *Field[int16]     { return newField[int16](name, KindInt16, "int16") }
func Int32(name string) *Field[int32]     { return newField[int32](name, KindInt32, "int32") }
func Int64(name string) *Field[int64]     { return newField[int64](name, KindInt64, "int64") }
func Uint(name string) *Field[uint]       { return newField[uint](name, KindUint, "uint") }
func Uint8(name string) *Field[uint8]     { return newField[uint8](name, KindUint8, "uint8") }
func Uint16(name string) *Field[uint16]   { return newField[uint16](name, KindUint16, "uint16") }
func Uint32(name string) *Field[uint32]   { return newField[uint32](name, KindUint32, "uint32") }
func Uint64(name string) *Field[uint64]   { return newField[uint64](name, KindUint64, "uint64") }
func Float32(name string) *Field[float32] { return newField[float32](name, KindFloat32, "float32") }
func Float64(name string) *Field[float64] { return newField[float64](name, KindFloat64, "float64") }
func String(name string) *Field[string]   { return newField[string](name, KindString, "string") }
func Bytes(name string) *Field[[]byte]    { return newField[[]byte](name, KindBytes, "[]byte") }
func Bool(name string) *Field[bool]       { return newField[bool](name, KindBool, "bool") }
func Duration(name string) *Field[time.Duration] {
	return newField[time.Duration](name, KindDuration, "time.Duration")
}
func Time(name string) *Field[time.Time] {
	return newField[time.Time](name, KindTime, "time.Time")
}
