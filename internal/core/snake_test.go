package core

import "testing"

func TestToScreamingSnake(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"Port":         "PORT",
		"DSN":          "DSN",
		"AllowedHosts": "ALLOWED_HOSTS",
		"HTTPServer":   "HTTP_SERVER",
		"camelCase":    "CAMEL_CASE",
		"ID":           "ID",
		"UserID":       "USER_ID",
		"OAuth2Token":  "O_AUTH2_TOKEN",
	}
	for in, want := range cases {
		if got := toScreamingSnake(in); got != want {
			t.Errorf("toScreamingSnake(%q) = %q, want %q", in, got, want)
		}
	}
}
