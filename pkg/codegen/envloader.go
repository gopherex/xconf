package codegen

import (
	"fmt"
	"strings"

	"github.com/gopherex/xconf"
)

// renderLoadFromEnv emits a typed LoadFromEnv function that reads
// os.Getenv-style values and assigns them directly to the target struct.
// No reflection is used for the env path. Defaults declared in the schema
// are applied inline before env lookup. Groups bound via BindLoader are
// populated by calling the bound loader (its result wins). Groups bound
// via BindType (without BindLoader) are recursed into. Inline groups are
// recursed into. Validators are skipped — callers wanting validation
// should use the reflective Load path which honours FieldDesc.Validators.
//
// Generated signature:
//
//	func LoadFromEnv() (*RootType, error)
func (r *renderer) renderLoadFromEnv(opts Options, rootType string, root xconf.FieldDesc) string {
	var body strings.Builder
	r.emitGroupEnv(&body, root, "cfg", "")
	return fmt.Sprintf(`// LoadFromEnv fills a %s purely from environment variables, with no
// reflection on the hot path. Schema defaults are applied first; env values
// override them. Schema validators are run via load.Validate after parsing
// (cold path: validation walks via reflection).
func LoadFromEnv() (*%s, error) {
	cfg := &%s{}
	var err error
	_ = err
%s	if verr := load.Validate(%s, cfg); verr != nil {
		return nil, verr
	}
	return cfg, nil
}
`, rootType, rootType, rootType, body.String(), opts.SchemaExpr)
}

func (r *renderer) emitGroupEnv(b *strings.Builder, g xconf.FieldDesc, targetExpr, indent string) {
	for _, child := range g.Children {
		fieldExpr := targetExpr + "." + child.Name
		switch {
		case child.Kind == xconf.KindGroup && child.BindLoader != "":
			r.emitBoundLoader(b, child, fieldExpr, indent)
		case child.Kind == xconf.KindGroup:
			r.emitGroupEnv(b, child, fieldExpr, indent)
		default:
			r.emitLeafEnv(b, child, fieldExpr, indent)
		}
	}
}

func (r *renderer) emitBoundLoader(b *strings.Builder, g xconf.FieldDesc, fieldExpr, indent string) {
	pkg, fn, _ := splitQualified(g.BindLoader)
	r.addImport(pkg, "")
	fmt.Fprintf(b, "%s\tif loaded, lerr := %s.%s(); lerr != nil {\n", indent, lastSegment(pkg), fn)
	fmt.Fprintf(b, "%s\t\treturn nil, lerr\n", indent)
	fmt.Fprintf(b, "%s\t} else {\n", indent)
	fmt.Fprintf(b, "%s\t\t%s = *loaded\n", indent, fieldExpr)
	fmt.Fprintf(b, "%s\t}\n", indent)
}

func (r *renderer) emitLeafEnv(b *strings.Builder, d xconf.FieldDesc, fieldExpr, indent string) {
	// Apply default first.
	if d.HasDefault {
		if def := renderLiteral(d, d.Default, r); def != "" {
			fmt.Fprintf(b, "%s\t%s = %s\n", indent, fieldExpr, def)
		}
	}
	if d.Env == "" {
		return
	}
	r.needsOS = true
	fmt.Fprintf(b, "%s\tif v, ok := os.LookupEnv(%q); ok {\n", indent, d.Env)
	r.emitParseAssign(b, d, fieldExpr, "v", indent+"\t")
	fmt.Fprintf(b, "%s\t}\n", indent)
}

func (r *renderer) emitParseAssign(b *strings.Builder, d xconf.FieldDesc, fieldExpr, src, indent string) {
	switch d.Kind {
	case xconf.KindString:
		fmt.Fprintf(b, "%s\t%s = %s\n", indent, fieldExpr, src)
	case xconf.KindBytes:
		fmt.Fprintf(b, "%s\t%s = []byte(%s)\n", indent, fieldExpr, src)
	case xconf.KindInt, xconf.KindInt8, xconf.KindInt16, xconf.KindInt32, xconf.KindInt64:
		r.needsStrconv = true
		fmt.Fprintf(b, "%s\tn, perr := strconv.ParseInt(%s, 10, 64)\n", indent, src)
		fmt.Fprintf(b, "%s\tif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s\t%s = %s(n)\n", indent, fieldExpr, d.GoType)
	case xconf.KindUint, xconf.KindUint8, xconf.KindUint16, xconf.KindUint32, xconf.KindUint64:
		r.needsStrconv = true
		fmt.Fprintf(b, "%s\tn, perr := strconv.ParseUint(%s, 10, 64)\n", indent, src)
		fmt.Fprintf(b, "%s\tif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s\t%s = %s(n)\n", indent, fieldExpr, d.GoType)
	case xconf.KindFloat32, xconf.KindFloat64:
		r.needsStrconv = true
		fmt.Fprintf(b, "%s\tf, perr := strconv.ParseFloat(%s, 64)\n", indent, src)
		fmt.Fprintf(b, "%s\tif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s\t%s = %s(f)\n", indent, fieldExpr, d.GoType)
	case xconf.KindBool:
		r.needsStrconv = true
		fmt.Fprintf(b, "%s\tbb, perr := strconv.ParseBool(%s)\n", indent, src)
		fmt.Fprintf(b, "%s\tif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s\t%s = bb\n", indent, fieldExpr)
	case xconf.KindDuration:
		r.needsTime = true
		fmt.Fprintf(b, "%s\tdv, perr := time.ParseDuration(%s)\n", indent, src)
		fmt.Fprintf(b, "%s\tif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s\t%s = dv\n", indent, fieldExpr)
	case xconf.KindTime:
		r.needsTime = true
		fmt.Fprintf(b, "%s\ttv, perr := time.Parse(time.RFC3339, %s)\n", indent, src)
		fmt.Fprintf(b, "%s\tif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s\t%s = tv\n", indent, fieldExpr)
	case xconf.KindSlice:
		r.emitSliceParse(b, d, fieldExpr, src, indent)
	case xconf.KindMap:
		r.emitMapParse(b, d, fieldExpr, src, indent)
	}
}

func (r *renderer) emitSliceParse(b *strings.Builder, d xconf.FieldDesc, fieldExpr, src, indent string) {
	r.needsStrings = true
	sep := d.EnvSplit
	if sep == "" {
		sep = ","
	}
	fmt.Fprintf(b, "%s\tparts := strings.Split(%s, %q)\n", indent, src, sep)
	fmt.Fprintf(b, "%s\tout := make(%s, 0, len(parts))\n", indent, d.GoType)
	fmt.Fprintf(b, "%s\tfor _, p := range parts {\n", indent)
	fmt.Fprintf(b, "%s\t\tp = strings.TrimSpace(p)\n", indent)
	elemDesc := xconf.FieldDesc{Kind: d.ElemKind, GoType: d.ElemGoType}
	r.emitParseToVar(b, elemDesc, "ev", "p", indent+"\t\t")
	fmt.Fprintf(b, "%s\t\tout = append(out, ev)\n", indent)
	fmt.Fprintf(b, "%s\t}\n", indent)
	fmt.Fprintf(b, "%s\t%s = out\n", indent, fieldExpr)
}

func (r *renderer) emitMapParse(b *strings.Builder, d xconf.FieldDesc, fieldExpr, src, indent string) {
	r.needsStrings = true
	entrySep := d.EnvSplit
	if entrySep == "" {
		entrySep = ","
	}
	kvSep := d.KVSplit
	if kvSep == "" {
		kvSep = "="
	}
	fmt.Fprintf(b, "%s\tout := %s{}\n", indent, d.GoType)
	fmt.Fprintf(b, "%s\tfor _, e := range strings.Split(%s, %q) {\n", indent, src, entrySep)
	fmt.Fprintf(b, "%s\t\tk, val, found := strings.Cut(strings.TrimSpace(e), %q)\n", indent, kvSep)
	fmt.Fprintf(b, "%s\t\tif !found { return nil, fmt.Errorf(\"malformed map entry %%q\", e) }\n", indent)
	r.addImport("fmt", "")
	keyDesc := xconf.FieldDesc{Kind: d.KeyKind, GoType: d.KeyGoType}
	valDesc := xconf.FieldDesc{Kind: d.ElemKind, GoType: d.ElemGoType}
	r.emitParseToVar(b, keyDesc, "kv", "strings.TrimSpace(k)", indent+"\t\t")
	r.emitParseToVar(b, valDesc, "vv", "strings.TrimSpace(val)", indent+"\t\t")
	fmt.Fprintf(b, "%s\t\tout[kv] = vv\n", indent)
	fmt.Fprintf(b, "%s\t}\n", indent)
	fmt.Fprintf(b, "%s\t%s = out\n", indent, fieldExpr)
}

// emitParseToVar emits parsing into a local variable instead of a struct field.
func (r *renderer) emitParseToVar(b *strings.Builder, d xconf.FieldDesc, varName, src, indent string) {
	switch d.Kind {
	case xconf.KindString:
		fmt.Fprintf(b, "%s%s := %s\n", indent, varName, src)
	case xconf.KindInt, xconf.KindInt8, xconf.KindInt16, xconf.KindInt32, xconf.KindInt64:
		r.needsStrconv = true
		fmt.Fprintf(b, "%sn, perr := strconv.ParseInt(%s, 10, 64)\n", indent, src)
		fmt.Fprintf(b, "%sif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s%s := %s(n)\n", indent, varName, d.GoType)
	case xconf.KindUint, xconf.KindUint8, xconf.KindUint16, xconf.KindUint32, xconf.KindUint64:
		r.needsStrconv = true
		fmt.Fprintf(b, "%sn, perr := strconv.ParseUint(%s, 10, 64)\n", indent, src)
		fmt.Fprintf(b, "%sif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s%s := %s(n)\n", indent, varName, d.GoType)
	case xconf.KindFloat32, xconf.KindFloat64:
		r.needsStrconv = true
		fmt.Fprintf(b, "%sf, perr := strconv.ParseFloat(%s, 64)\n", indent, src)
		fmt.Fprintf(b, "%sif perr != nil { return nil, perr }\n", indent)
		fmt.Fprintf(b, "%s%s := %s(f)\n", indent, varName, d.GoType)
	case xconf.KindBool:
		r.needsStrconv = true
		fmt.Fprintf(b, "%s%s, perr := strconv.ParseBool(%s)\n", indent, varName, src)
		fmt.Fprintf(b, "%sif perr != nil { return nil, perr }\n", indent)
	default:
		fmt.Fprintf(b, "%s%s := %s // unsupported elem kind: passthrough\n", indent, varName, src)
	}
}

// renderLiteral converts a default value into a Go literal expression.
func renderLiteral(d xconf.FieldDesc, v any, r *renderer) string {
	switch d.Kind {
	case xconf.KindString:
		if s, ok := v.(string); ok {
			return fmt.Sprintf("%q", s)
		}
	case xconf.KindBool:
		if b, ok := v.(bool); ok {
			return fmt.Sprintf("%v", b)
		}
	case xconf.KindInt, xconf.KindInt8, xconf.KindInt16, xconf.KindInt32, xconf.KindInt64,
		xconf.KindUint, xconf.KindUint8, xconf.KindUint16, xconf.KindUint32, xconf.KindUint64:
		return fmt.Sprintf("%s(%v)", d.GoType, v)
	case xconf.KindFloat32, xconf.KindFloat64:
		return fmt.Sprintf("%s(%v)", d.GoType, v)
	case xconf.KindDuration:
		r.needsTime = true
		if dv, ok := v.(interface{ Nanoseconds() int64 }); ok {
			return fmt.Sprintf("time.Duration(%d)", dv.Nanoseconds())
		}
	}
	return ""
}
