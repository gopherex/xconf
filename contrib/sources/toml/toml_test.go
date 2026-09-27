package toml_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	st "github.com/gopherex/xconf/contrib/sources/toml"
)

func TestTOMLNativeContainers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte("big = 9007199254740993\nports = [1, 2]\n[db]\nhost = 'localhost'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := sp.NewSchema(sp.ID("test", "toml", sp.Ver(1, 0, 0))).Fields(sp.Int64("big"), sp.List("ports", sp.Int64("")), sp.Object("db", sp.Str("host"))).MustBuild()
	v, err := x.LoadAs[map[string]any](context.Background(), s, st.File(p))
	if err != nil || v["big"] != int64(9007199254740993) || len(v["ports"].([]any)) != 2 {
		t.Fatal(v, err)
	}
}
