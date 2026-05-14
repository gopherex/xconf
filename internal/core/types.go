// Package core holds the implementation of xconf. The public surface lives
// in the parent xconf package as type aliases and constructor wrappers.
package core

// Kind classifies a node for the codegen / reflection layers.
type Kind int

const (
	KindInvalid Kind = iota
	KindInt
	KindInt8
	KindInt16
	KindInt32
	KindInt64
	KindUint
	KindUint8
	KindUint16
	KindUint32
	KindUint64
	KindFloat32
	KindFloat64
	KindString
	KindBytes
	KindBool
	KindDuration
	KindTime
	KindSlice
	KindMap
	KindGroup
)

// FieldDesc is the untyped, serializable description of a Node.
type FieldDesc struct {
	Name        string
	Kind        Kind
	GoType      string
	Env         string
	EnvSplit    string
	Default     any
	HasDefault  bool
	Required    bool
	Description string

	// Group only.
	Children   []FieldDesc
	BindType   string
	BindLoader string

	// Slice + Map: value element.
	ElemKind   Kind
	ElemGoType string

	// Map only: key element.
	KeyKind   Kind
	KeyGoType string

	// Map only: separator inside a single "k=v" entry. Entry-pair separator
	// is shared with Slice via EnvSplit.
	KVSplit string

	// Validators is a type-erased view of the field's validators, suitable
	// for runtime binders. Codegen does not consume this (validators are
	// linked via source code references in generated code).
	Validators []func(any) error

	// LoaderFn is the runtime-loader adapter for a Group with BindLoader.
	// It accepts a reflect.Value pointing to the field's struct and
	// populates it from the bound external loader. Non-nil only when the
	// schema attached a loader via WithLoader.
	LoaderFn func(any) error
}

// Node is the common interface for fields and groups. The unexported
// describe method makes Node a closed sum type: only field and schema types
// in this package can satisfy it.
type Node interface {
	describe() FieldDesc
}

// Validator is a typed validation function.
type Validator[T any] func(value T) error
