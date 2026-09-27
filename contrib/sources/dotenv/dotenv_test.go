package dotenv_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/dotenv"
	"github.com/gopherex/xconf/contrib/sources/env"
)

func TestDotEnvDoesNotMutateProcess(t *testing.T) {
	t.Setenv("APP_N", "9")
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte("APP_N=2\nAPP_TEXT=''\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := sp.NewSchema(sp.ID("test", "dotenv", sp.Ver(1, 0, 0))).Coerce().Fields(sp.Int64("n"), sp.Str("text").Default("default")).MustBuild()
	v, err := x.LoadAs[map[string]any](context.Background(), s, dotenv.File(p, env.Prefix("APP_")))
	if err != nil || v["n"] != int64(2) || v["text"] != "" || os.Getenv("APP_N") != "9" {
		t.Fatal(v, err)
	}
}
