package core

import "reflect"

// SliceField is a typed slice configuration leaf.
type SliceField[T any] struct {
	name        string
	def         *[]T
	env         string
	envSplit    string
	required    bool
	description string
	validators  []Validator[[]T]
}

func (s *SliceField[T]) Default(v []T) *SliceField[T]        { s.def = &v; return s }
func (s *SliceField[T]) Env(name string) *SliceField[T]      { s.env = name; return s }
func (s *SliceField[T]) EnvSplit(sep string) *SliceField[T]  { s.envSplit = sep; return s }
func (s *SliceField[T]) Required() *SliceField[T]            { s.required = true; return s }
func (s *SliceField[T]) Description(d string) *SliceField[T] { s.description = d; return s }
func (s *SliceField[T]) Validate(v Validator[[]T]) *SliceField[T] {
	s.validators = append(s.validators, v)
	return s
}

func (s *SliceField[T]) describe() FieldDesc {
	var zero T
	elemKind, elemGoType := inferKind(reflect.TypeOf(zero))
	d := FieldDesc{
		Name:        s.name,
		Kind:        KindSlice,
		GoType:      "[]" + elemGoType,
		Env:         s.env,
		EnvSplit:    s.envSplit,
		Required:    s.required,
		Description: s.description,
		ElemKind:    elemKind,
		ElemGoType:  elemGoType,
		Validators:  eraseValidators(s.validators),
	}
	if s.def != nil {
		d.Default = *s.def
		d.HasDefault = true
	}
	return d
}

// Slice constructs a typed slice field. EnvSplit(",") enables loading from a
// single env var split by separator.
func Slice[T any](name string) *SliceField[T] {
	return &SliceField[T]{name: name}
}

func inferKind(t reflect.Type) (Kind, string) {
	if t == nil {
		return KindInvalid, ""
	}
	if t.PkgPath() == "time" {
		switch t.Name() {
		case "Duration":
			return KindDuration, "time.Duration"
		case "Time":
			return KindTime, "time.Time"
		}
	}
	switch t.Kind() {
	case reflect.Int:
		return KindInt, "int"
	case reflect.Int8:
		return KindInt8, "int8"
	case reflect.Int16:
		return KindInt16, "int16"
	case reflect.Int32:
		return KindInt32, "int32"
	case reflect.Int64:
		return KindInt64, "int64"
	case reflect.Uint:
		return KindUint, "uint"
	case reflect.Uint8:
		return KindUint8, "uint8"
	case reflect.Uint16:
		return KindUint16, "uint16"
	case reflect.Uint32:
		return KindUint32, "uint32"
	case reflect.Uint64:
		return KindUint64, "uint64"
	case reflect.Float32:
		return KindFloat32, "float32"
	case reflect.Float64:
		return KindFloat64, "float64"
	case reflect.String:
		return KindString, "string"
	case reflect.Bool:
		return KindBool, "bool"
	}
	return KindInvalid, t.String()
}
