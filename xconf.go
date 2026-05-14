// Package xconf provides a typed, declarative configuration DSL.
//
// Schemas are built with fluent constructors (Int, String, Duration, Slice,
// Group, GroupAs, ...) and compose into a tree of Nodes. The same schema can
// be:
//   - serialized via Describe() and consumed by the xconfgen code generator
//     to produce a typed *Config struct plus a Load() function with zero
//     runtime reflection.
//   - bound at runtime via reflection (planned).
//
// Validators live in github.com/gopherex/xconf/pkg/validate.
// Implementation lives in internal/core; this file is the public facade.
package xconf

import (
	"time"

	"github.com/gopherex/xconf/internal/core"
)

// --- Re-exported types ---

type (
	Kind                          = core.Kind
	FieldDesc                     = core.FieldDesc
	Node                          = core.Node
	Validator[T any]              = core.Validator[T]
	Field[T any]                  = core.Field[T]
	SliceField[T any]             = core.SliceField[T]
	MapField[K comparable, V any] = core.MapField[K, V]
	Schema                        = core.Schema
)

// --- Re-exported kinds ---

const (
	KindInvalid  = core.KindInvalid
	KindInt      = core.KindInt
	KindInt8     = core.KindInt8
	KindInt16    = core.KindInt16
	KindInt32    = core.KindInt32
	KindInt64    = core.KindInt64
	KindUint     = core.KindUint
	KindUint8    = core.KindUint8
	KindUint16   = core.KindUint16
	KindUint32   = core.KindUint32
	KindUint64   = core.KindUint64
	KindFloat32  = core.KindFloat32
	KindFloat64  = core.KindFloat64
	KindString   = core.KindString
	KindBytes    = core.KindBytes
	KindBool     = core.KindBool
	KindDuration = core.KindDuration
	KindTime     = core.KindTime
	KindSlice    = core.KindSlice
	KindMap      = core.KindMap
	KindGroup    = core.KindGroup
)

// --- Field constructors ---

func Int(name string) *Field[int]                { return core.Int(name) }
func Int8(name string) *Field[int8]              { return core.Int8(name) }
func Int16(name string) *Field[int16]            { return core.Int16(name) }
func Int32(name string) *Field[int32]            { return core.Int32(name) }
func Int64(name string) *Field[int64]            { return core.Int64(name) }
func Uint(name string) *Field[uint]              { return core.Uint(name) }
func Uint8(name string) *Field[uint8]            { return core.Uint8(name) }
func Uint16(name string) *Field[uint16]          { return core.Uint16(name) }
func Uint32(name string) *Field[uint32]          { return core.Uint32(name) }
func Uint64(name string) *Field[uint64]          { return core.Uint64(name) }
func Float32(name string) *Field[float32]        { return core.Float32(name) }
func Float64(name string) *Field[float64]        { return core.Float64(name) }
func String(name string) *Field[string]          { return core.String(name) }
func Bytes(name string) *Field[[]byte]           { return core.Bytes(name) }
func Bool(name string) *Field[bool]              { return core.Bool(name) }
func Duration(name string) *Field[time.Duration] { return core.Duration(name) }
func Time(name string) *Field[time.Time]         { return core.Time(name) }
func Slice[T any](name string) *SliceField[T]    { return core.Slice[T](name) }
func Map[K comparable, V any](name string) *MapField[K, V] {
	return core.Map[K, V](name)
}

// --- Schema constructors / composition ---

func Define(name string, fields ...Node) *Schema { return core.Define(name, fields...) }
func Group(name string, fields ...Node) *Schema  { return core.Group(name, fields...) }
func GroupAs[T any](name string, fields ...Node) *Schema {
	return core.GroupAs[T](name, fields...)
}
func Embed(name string, sub *Schema) *Schema { return core.Embed(name, sub) }
func WithLoader[T any](s *Schema, loader func() (*T, error)) *Schema {
	return core.WithLoader(s, loader)
}
