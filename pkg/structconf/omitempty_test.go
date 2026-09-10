package structconf

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOmitEmptyTypes(t *testing.T) {
	empty := ""
	zero := 0
	positive := 2
	validURL := "https://example.com"
	nilSlice := []int(nil)
	cases := []struct {
		name    string
		value   any
		tag     string
		wantErr bool
	}{
		{"string zero", "", "omitempty,url", false},
		{"string valid", "https://example.com", "omitempty,url", false},
		{"string invalid", "invalid", "omitempty,url", true},
		{"whitespace", "   ", "omitempty,url", true},
		{"bool zero", false, "omitempty,oneof=true", false},
		{"bool present", true, "omitempty,oneof=false", true},
		{"int zero", 0, "omitempty,min=1", false},
		{"int invalid", -1, "omitempty,min=1", true},
		{"int valid", 1, "omitempty,min=1", false},
		{"uint zero", uint(0), "omitempty,min=1", false},
		{"uint invalid", uint(1), "omitempty,min=2", true},
		{"float zero", 0.0, "omitempty,min=1", false},
		{"float invalid", 0.5, "omitempty,min=1", true},
		{"duration zero", time.Duration(0), "omitempty,min=1", false},
		{"duration invalid", -time.Second, "omitempty,min=1", true},
		{"time zero", time.Time{}, "omitempty,required", false},
		{"time present", time.Unix(1, 0), "omitempty,required", false},
		{"slice nil", []string(nil), "omitempty,min=1", false},
		{"slice empty", []string{}, "omitempty,min=1", true},
		{"slice present", []string{"x"}, "omitempty,min=1", false},
		{"map nil", map[string]int(nil), "omitempty,min=1", false},
		{"map empty", map[string]int{}, "omitempty,min=1", true},
		{"map present", map[string]int{"x": 1}, "omitempty,min=1", false},
		{"array zero", [2]int{}, "omitempty,min=3", false},
		{"array present", [2]int{1}, "omitempty,min=3", true},
		{"pointer nil", (*string)(nil), "omitempty,url", false},
		{"pointer empty string", &empty, "omitempty,url", true},
		{"pointer zero int", &zero, "omitempty,min=1", true},
		{"pointer valid int", &positive, "omitempty,min=1", false},
		{"pointer valid string", &validURL, "omitempty,url", false},
		{"pointer nil slice", &nilSlice, "omitempty,min=1", false},
		{"pointer required presence", &zero, "omitempty,required", false},
		{"pointer required OR presence", &zero, "omitempty,required|gt=0", false},
		{"nil interface", nil, "omitempty,url", false},
		{"OR empty", "", "omitempty,hostname|ip", false},
		{"OR valid", "127.0.0.1", "omitempty,hostname|ip", false},
		{"OR invalid", "bad host!", "omitempty,hostname|ip", true},
		{"required first", "", "required,omitempty,url", true},
		{"required after", "", "omitempty,required,url", false},
		{"url first", "", "url,omitempty", true},
		{"spaces in tag", "", " omitempty , url ", false},
		{"without omit", "", "url", true},
		{"omit in OR rejected", "bad", "omitempty|url", true},
		{"omit after valid OR rejected", "https://example.com", "url|omitempty", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runRules(reflect.ValueOf(tc.value), reflect.Value{}, tc.tag, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("value=%#v tag=%q: err=%v, wantErr=%v", tc.value, tc.tag, err, tc.wantErr)
			}
		})
	}
	// Named scalar types and every numeric width use the same zero semantics.
	type namedString string
	type namedInt int
	for _, value := range []any{namedString(""), namedInt(0), int8(0), int16(0), int32(0), int64(0), uint8(0), uint16(0), uint32(0), uint64(0), float32(0)} {
		t.Run(reflect.TypeOf(value).String(), func(t *testing.T) {
			if err := runRules(reflect.ValueOf(value), reflect.Value{}, "omitempty,min=1", nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOmitEmptyDive(t *testing.T) {
	cases := []struct {
		name         string
		value        any
		tag, wantErr string
	}{
		{"slice skip elements", []string{"", "https://example.com"}, "dive,omitempty,url", ""},
		{"slice invalid element", []string{"", "bad"}, "dive,omitempty,url", "[1]:"},
		{"array skip elements", [2]int{0, 2}, "dive,omitempty,min=1", ""},
		{"array invalid element", [2]int{0, -1}, "dive,omitempty,min=1", "[1]:"},
		{"map skip elements", map[string]string{"empty": "", "valid": "https://example.com"}, "dive,omitempty,url", ""},
		{"map invalid element", map[string]string{"bad": "invalid"}, "dive,omitempty,url", "[bad]:"},
		{"outer omit does not skip elements", []string{""}, "omitempty,dive,url", "[0]:"},
		{"nested skip", [][]string{nil, {"", "https://example.com"}}, "dive,omitempty,min=1,dive,omitempty,url", ""},
		{"nested invalid", [][]string{{"", "bad"}}, "dive,dive,omitempty,url", "[0]: [1]:"},
		{"nested empty present", [][]string{{}}, "dive,omitempty,min=1,dive,url", "[0]:"},
		{"nested map invalid", map[string][]int{"ports": {0, -1}}, "dive,dive,omitempty,min=1", "[ports]: [1]:"},
		{"interface elements", []any{nil, "", "https://example.com"}, "dive,omitempty,url", "[1]:"},
		{"interface typed nil", []any{(*string)(nil), []string(nil)}, "dive,omitempty,min=1", ""},
		{"pointer elements", []*int{nil, new(int)}, "dive,omitempty,min=1", "[1]:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runRules(reflect.ValueOf(tc.value), reflect.Value{}, tc.tag, nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestOmitEmptyStructs(t *testing.T) {
	type section struct {
		URL     string `mapstructure:"url" validate:"required,url"`
		Enabled bool   `mapstructure:"enabled"`
	}
	type config struct {
		Section section  `mapstructure:"section" validate:"omitempty"`
		Pointer *section `mapstructure:"pointer" validate:"omitempty"`
		Other   string   `mapstructure:"other" validate:"url"`
	}
	cases := []struct {
		name    string
		value   config
		wantErr string
	}{
		{"zero section", config{Other: "https://example.com"}, ""},
		{"present section", config{Section: section{Enabled: true}, Other: "https://example.com"}, "section.url: required"},
		{"present pointer section", config{Pointer: &section{}, Other: "https://example.com"}, "pointer.url: required"},
		{"valid section", config{Section: section{URL: "https://example.com"}, Other: "https://example.com"}, ""},
		{"sibling still checked", config{}, "other:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStruct(reflect.ValueOf(tc.value), nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestOmitEmptyConditionalRules(t *testing.T) {
	parent := reflect.ValueOf(struct{ Mode string }{Mode: "tls"})
	for _, tc := range []struct {
		tag     string
		wantErr bool
	}{
		{"required_if=Mode tls,omitempty,url", true},
		{"required_if=Mode plain,omitempty,url", false},
		{"omitempty,required_if=Mode tls,url", false},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			err := runRules(reflect.ValueOf(""), parent, tc.tag, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestOmitEmptyDiveStructs(t *testing.T) {
	type section struct {
		URL     string `mapstructure:"url" validate:"required,url"`
		Enabled bool   `mapstructure:"enabled"`
	}
	for _, tc := range []struct {
		name    string
		value   any
		wantErr string
	}{
		{"zero struct skipped", []section{{}, {URL: "https://example.com"}}, ""},
		{"nonzero struct checked", []section{{}, {Enabled: true}}, "[1]: url: required"},
		{"nil pointer skipped", []*section{nil, {URL: "https://example.com"}}, ""},
		{"non-nil pointer checked", []*section{nil, {}}, "[1]: url: required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runRules(reflect.ValueOf(tc.value), reflect.Value{}, "dive,omitempty", nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("err=%v, want %q", err, tc.wantErr)
			}
		})
	}
}
