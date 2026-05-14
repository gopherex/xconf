package core

import "reflect"

// MapField is a typed map configuration leaf.
//
// Env decoding convention: a single env var holds entries separated by
// EnvSplit (default ","), each entry split into key/value by KVSplit
// (default "="). Example: "foo=1,bar=2".
type MapField[K comparable, V any] struct {
	name        string
	def         *map[K]V
	env         string
	envSplit    string
	kvSplit     string
	required    bool
	description string
	validators  []Validator[map[K]V]
}

func (m *MapField[K, V]) Default(v map[K]V) *MapField[K, V]   { m.def = &v; return m }
func (m *MapField[K, V]) Env(name string) *MapField[K, V]     { m.env = name; return m }
func (m *MapField[K, V]) EnvSplit(sep string) *MapField[K, V] { m.envSplit = sep; return m }
func (m *MapField[K, V]) KVSplit(sep string) *MapField[K, V]  { m.kvSplit = sep; return m }
func (m *MapField[K, V]) Required() *MapField[K, V]           { m.required = true; return m }
func (m *MapField[K, V]) Description(d string) *MapField[K, V] {
	m.description = d
	return m
}
func (m *MapField[K, V]) Validate(v Validator[map[K]V]) *MapField[K, V] {
	m.validators = append(m.validators, v)
	return m
}

func (m *MapField[K, V]) describe() FieldDesc {
	var zK K
	var zV V
	keyKind, keyGoType := inferKind(reflect.TypeOf(zK))
	valKind, valGoType := inferKind(reflect.TypeOf(zV))
	d := FieldDesc{
		Name:        m.name,
		Kind:        KindMap,
		GoType:      "map[" + keyGoType + "]" + valGoType,
		Env:         m.env,
		EnvSplit:    m.envSplit,
		KVSplit:     m.kvSplit,
		Required:    m.required,
		Description: m.description,
		KeyKind:     keyKind,
		KeyGoType:   keyGoType,
		ElemKind:    valKind,
		ElemGoType:  valGoType,
		Validators:  eraseValidators(m.validators),
	}
	if m.def != nil {
		d.Default = *m.def
		d.HasDefault = true
	}
	return d
}

// Map constructs a typed map field. EnvSplit / KVSplit configure parsing
// from a single env var (defaults are codegen-defined: "," and "=").
func Map[K comparable, V any](name string) *MapField[K, V] {
	return &MapField[K, V]{name: name}
}
