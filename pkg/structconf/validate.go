package structconf

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gopherex/xconf/pkg/validate"
)

// validateStruct walks rv and enforces every field's `validate` tag. path is
// the dotted mapstructure path for error messages; parent is the struct that
// owns rv's fields (same as rv) so cross-field rules can resolve siblings.
func validateStruct(rv reflect.Value, path []string) error {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		ms, ok := parseMapstructure(sf)
		if !ok {
			continue
		}
		fv := rv.Field(i)
		ft := sf.Type
		if ft.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
			ft = ft.Elem()
		}

		cpath := path
		if !ms.squash {
			cpath = append(append([]string(nil), path...), ms.name)
		}

		if ft.Kind() == reflect.Struct && ft != timeType {
			if err := validateStruct(fv, cpath); err != nil {
				return err
			}
			continue
		}

		tag, has := sf.Tag.Lookup("validate")
		if !has || strings.TrimSpace(tag) == "" {
			continue
		}
		if err := runRules(fv, rv, tag); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(cpath, "."), err)
		}
	}
	return nil
}

// runRules applies a go-playground-style validate tag. Top-level separator is
// comma (AND). A `dive` token switches remaining rules to per-element mode for
// slices/arrays/maps. A rule containing '|' is an OR group.
func runRules(fv, parent reflect.Value, tag string) error {
	tokens := splitTopLevel(tag)
	for i, tok := range tokens {
		if tok == "dive" {
			return diveInto(fv, parent, tokens[i+1:])
		}
		if err := runToken(fv, parent, tok); err != nil {
			return err
		}
	}
	return nil
}

func diveInto(fv, parent reflect.Value, elemRules []string) error {
	switch fv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < fv.Len(); i++ {
			if err := runRuleList(fv.Index(i), parent, elemRules); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
	case reflect.Map:
		for _, k := range fv.MapKeys() {
			if err := runRuleList(fv.MapIndex(k), parent, elemRules); err != nil {
				return fmt.Errorf("[%v]: %w", k.Interface(), err)
			}
		}
	default:
		return fmt.Errorf("dive on non-collection %v", fv.Kind())
	}
	return nil
}

func runRuleList(fv, parent reflect.Value, tokens []string) error {
	for _, tok := range tokens {
		if tok == "dive" {
			return diveInto(fv, parent, nil) // nested dive: no further rules supported
		}
		if err := runToken(fv, parent, tok); err != nil {
			return err
		}
	}
	return nil
}

func runToken(fv, parent reflect.Value, rule string) error {
	if strings.Contains(rule, "|") {
		var errs []string
		for _, alt := range strings.Split(rule, "|") {
			alt = strings.TrimSpace(alt)
			if alt == "" {
				continue
			}
			if err := runRule(fv, parent, alt); err == nil {
				return nil
			} else {
				errs = append(errs, err.Error())
			}
		}
		return fmt.Errorf("no alternative matched (%s)", strings.Join(errs, "; "))
	}
	return runRule(fv, parent, rule)
}

// runRule applies a single rule token (name or name=param) to fv.
func runRule(fv, parent reflect.Value, rule string) error {
	rule = strings.TrimSpace(rule)
	name, param, _ := strings.Cut(rule, "=")
	name = strings.TrimSpace(name)
	param = strings.TrimSpace(param)

	switch name {
	case "", "omitempty", "structonly", "dive":
		return nil
	case "required":
		if fv.IsZero() {
			return fmt.Errorf("required")
		}
		return nil

	// --- length / magnitude (kind-aware) ---
	case "min", "max", "len", "gt", "gte", "lt", "lte", "eq", "ne":
		return cmpRule(fv, param, name)

	// --- set membership ---
	case "oneof":
		return validate.OneOf(splitOneof(param)...)(asString(fv))

	// --- string formats (reuse pkg/validate where it exists) ---
	case "email":
		return validate.Email()(asString(fv))
	case "url", "uri", "http_url":
		return validate.URL()(asString(fv))
	case "contains":
		return validate.Contains(param)(asString(fv))
	case "excludes":
		if strings.Contains(asString(fv), param) {
			return fmt.Errorf("must not contain %q", param)
		}
		return nil
	case "startswith":
		return validate.HasPrefix(param)(asString(fv))
	case "endswith":
		return validate.HasSuffix(param)(asString(fv))
	case "alpha":
		return reMatch(reAlpha, asString(fv), "alpha")
	case "alphanum":
		return reMatch(reAlphanum, asString(fv), "alphanum")
	case "numeric":
		return reMatch(reNumeric, asString(fv), "numeric")
	case "number":
		return reMatch(reNumber, asString(fv), "number")
	case "uuid":
		return reMatch(reUUID, asString(fv), "uuid")
	case "base64":
		if _, err := base64.StdEncoding.DecodeString(asString(fv)); err != nil {
			return fmt.Errorf("invalid base64")
		}
		return nil
	case "json":
		if !json.Valid([]byte(asString(fv))) {
			return fmt.Errorf("invalid json")
		}
		return nil
	case "e164":
		return reMatch(reE164, asString(fv), "e164")
	case "datetime":
		if param == "" {
			return fmt.Errorf("datetime requires a layout")
		}
		if _, err := time.Parse(param, asString(fv)); err != nil {
			return fmt.Errorf("datetime %q != layout %q", asString(fv), param)
		}
		return nil

	// --- collection ---
	case "unique":
		return uniqueRule(fv)

	// --- network formats ---
	case "hostname", "hostname_rfc1123":
		return checkHostname(asString(fv))
	case "fqdn":
		return checkFQDN(asString(fv))
	case "ip":
		return checkIP(asString(fv), 0)
	case "ipv4":
		return checkIP(asString(fv), 4)
	case "ipv6":
		return checkIP(asString(fv), 6)
	case "cidr":
		if _, _, err := net.ParseCIDR(asString(fv)); err != nil {
			return fmt.Errorf("invalid CIDR %q", asString(fv))
		}
		return nil
	case "mac":
		if _, err := net.ParseMAC(asString(fv)); err != nil {
			return fmt.Errorf("invalid MAC %q", asString(fv))
		}
		return nil
	case "hostname_port", "hostport":
		return checkHostPort(asString(fv))

	// --- cross-field ---
	case "eqfield", "nefield", "gtfield", "gtefield", "ltfield", "ltefield":
		return fieldCmp(fv, parent, param, name)
	case "required_with":
		return requiredWith(fv, parent, param, true, false)
	case "required_without":
		return requiredWith(fv, parent, param, false, false)
	case "required_with_all":
		return requiredWith(fv, parent, param, true, true)
	case "required_without_all":
		return requiredWith(fv, parent, param, false, true)
	case "required_if":
		return requiredIf(fv, parent, param, true)
	case "required_unless":
		return requiredIf(fv, parent, param, false)

	default:
		return fmt.Errorf("unsupported validate rule %q", name)
	}
}

// ---------------------------------------------------------------------------
// magnitude / length
// ---------------------------------------------------------------------------

func cmpRule(fv reflect.Value, param, op string) error {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return fmt.Errorf("%s: bad bound %q: %w", op, param, err)
	}
	switch fv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		l := fv.Len()
		return compareInt(float64(l), n, op, "length")
	}
	return compareInt(asFloat(fv), n, op, "value")
}

func compareInt(v, n float64, op, what string) error {
	switch op {
	case "min", "gte":
		if v < n {
			return fmt.Errorf("%s %v < %v", what, v, n)
		}
	case "max", "lte":
		if v > n {
			return fmt.Errorf("%s %v > %v", what, v, n)
		}
	case "len", "eq":
		if v != n {
			return fmt.Errorf("%s %v != %v", what, v, n)
		}
	case "ne":
		if v == n {
			return fmt.Errorf("%s must not be %v", what, n)
		}
	case "gt":
		if v <= n {
			return fmt.Errorf("%s %v must be > %v", what, v, n)
		}
	case "lt":
		if v >= n {
			return fmt.Errorf("%s %v must be < %v", what, v, n)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// cross-field
// ---------------------------------------------------------------------------

func siblingByName(parent reflect.Value, name string) (reflect.Value, error) {
	if parent.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("cross-field rule needs a struct parent")
	}
	fv := parent.FieldByName(name)
	if !fv.IsValid() {
		return reflect.Value{}, fmt.Errorf("unknown field %q", name)
	}
	return fv, nil
}

func fieldCmp(fv, parent reflect.Value, name, op string) error {
	other, err := siblingByName(parent, name)
	if err != nil {
		return err
	}
	a, b := asComparable(fv), asComparable(other)
	switch op {
	case "eqfield":
		if a != b {
			return fmt.Errorf("must equal field %s", name)
		}
	case "nefield":
		if a == b {
			return fmt.Errorf("must not equal field %s", name)
		}
	case "gtfield", "gtefield", "ltfield", "ltefield":
		return compareInt(asFloat(fv), asFloat(other), strings.TrimSuffix(op, "field"), "value")
	}
	return nil
}

func requiredWith(fv, parent reflect.Value, param string, withPresent, all bool) error {
	names := strings.Fields(param)
	trigger := !all // for "all": start true (AND); for "any": start false (OR)
	for _, n := range names {
		other, err := siblingByName(parent, n)
		if err != nil {
			return err
		}
		present := !other.IsZero()
		cond := present == withPresent
		if all {
			trigger = trigger && cond
		} else {
			trigger = trigger || cond
		}
	}
	if trigger && fv.IsZero() {
		return fmt.Errorf("required (conditional on %s)", param)
	}
	return nil
}

func requiredIf(fv, parent reflect.Value, param string, ifEqual bool) error {
	parts := strings.Fields(param)
	if len(parts) < 2 {
		return fmt.Errorf("required_if/unless needs FIELD VALUE pairs")
	}
	match := true
	for i := 0; i+1 < len(parts); i += 2 {
		other, err := siblingByName(parent, parts[i])
		if err != nil {
			return err
		}
		if asString(other) != parts[i+1] {
			match = false
			break
		}
	}
	trigger := match == ifEqual
	if trigger && fv.IsZero() {
		return fmt.Errorf("required (%s)", param)
	}
	return nil
}

// ---------------------------------------------------------------------------
// collection
// ---------------------------------------------------------------------------

func uniqueRule(fv reflect.Value) error {
	if fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
		return fmt.Errorf("unique on non-slice %v", fv.Kind())
	}
	seen := make(map[string]struct{}, fv.Len())
	for i := 0; i < fv.Len(); i++ {
		k := fmt.Sprintf("%v", fv.Index(i).Interface())
		if _, dup := seen[k]; dup {
			return fmt.Errorf("duplicate element %q", k)
		}
		seen[k] = struct{}{}
	}
	return nil
}

// ---------------------------------------------------------------------------
// network helpers
// ---------------------------------------------------------------------------

func checkHostname(s string) error {
	if s == "" {
		return fmt.Errorf("empty hostname")
	}
	if len(s) > 253 {
		return fmt.Errorf("hostname %q too long", s)
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("invalid hostname label in %q", s)
		}
		for i, r := range label {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
				(r == '-' && i != 0 && i != len(label)-1)
			if !ok {
				return fmt.Errorf("invalid hostname %q", s)
			}
		}
	}
	return nil
}

func checkFQDN(s string) error {
	if err := checkHostname(s); err != nil {
		return err
	}
	if !strings.Contains(strings.TrimSuffix(s, "."), ".") {
		return fmt.Errorf("%q is not a FQDN", s)
	}
	return nil
}

func checkIP(s string, ver int) error {
	ip := net.ParseIP(s)
	if ip == nil {
		return fmt.Errorf("invalid IP %q", s)
	}
	switch ver {
	case 4:
		if ip.To4() == nil {
			return fmt.Errorf("%q is not IPv4", s)
		}
	case 6:
		if ip.To4() != nil {
			return fmt.Errorf("%q is not IPv6", s)
		}
	}
	return nil
}

func checkHostPort(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("invalid host:port %q: %w", s, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("invalid port in %q", s)
	}
	if ip := net.ParseIP(host); ip == nil {
		return checkHostname(host)
	}
	return nil
}

// ---------------------------------------------------------------------------
// regex formats
// ---------------------------------------------------------------------------

var (
	reAlpha    = regexp.MustCompile(`^[a-zA-Z]+$`)
	reAlphanum = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
	reNumeric  = regexp.MustCompile(`^[-+]?[0-9]+(?:\.[0-9]+)?$`)
	reNumber   = regexp.MustCompile(`^[0-9]+$`)
	reUUID     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reE164     = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)
)

func reMatch(re *regexp.Regexp, s, name string) error {
	if !re.MatchString(s) {
		return fmt.Errorf("value %q is not %s", s, name)
	}
	return nil
}

// ---------------------------------------------------------------------------
// tag tokenizing & value extraction
// ---------------------------------------------------------------------------

// splitTopLevel splits a validate tag by commas, ignoring commas inside single
// quotes (used by oneof='a b',c style params).
func splitTopLevel(tag string) []string {
	var out []string
	var b strings.Builder
	inQuote := false
	for _, r := range tag {
		switch {
		case r == '\'':
			inQuote = !inQuote
			b.WriteRune(r)
		case r == ',' && !inQuote:
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, strings.TrimSpace(b.String()))
	}
	return out
}

// splitOneof splits an oneof param by spaces, honoring single-quoted groups.
func splitOneof(param string) []string {
	var out []string
	var b strings.Builder
	inQuote := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range param {
		switch {
		case r == '\'':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

func asString(fv reflect.Value) string {
	if fv.Kind() == reflect.String {
		return fv.String()
	}
	if fv.IsValid() && fv.CanInterface() {
		return fmt.Sprintf("%v", fv.Interface())
	}
	return ""
}

func asComparable(fv reflect.Value) any {
	if fv.IsValid() && fv.CanInterface() {
		return fv.Interface()
	}
	return nil
}

func asFloat(fv reflect.Value) float64 {
	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(fv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(fv.Uint())
	case reflect.Float32, reflect.Float64:
		return fv.Float()
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return float64(fv.Len())
	}
	return 0
}
