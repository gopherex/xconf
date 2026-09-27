package yaml_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	sy "github.com/gopherex/xconf/contrib/sources/yaml"
)

func TestYAMLExactIntegersAndLocations(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("n: 9007199254740993\nlimits:\n  cpu: 18446744073709551615\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := sp.NewSchema(sp.ID("test", "yaml", sp.Ver(1, 0, 0))).Fields(sp.Int64("n"), sp.MapOf("limits", sp.UInt64("value"))).MustBuild()
	snap, err := x.Load(context.Background(), s, sy.File(p))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := x.Decode[map[string]any](snap)
	if v["n"] != int64(9007199254740993) || v["limits"].(map[string]any)["cpu"] != ^uint64(0) {
		t.Fatal(v)
	}
	origin := snap.Explain("limits", "cpu")
	if origin[0].Location.Line != 3 || origin[0].Location.Name != p {
		t.Fatal(origin)
	}
}
func TestYAMLRejectsAmbiguousDocuments(t *testing.T) {
	for _, raw := range []string{"null", "a: 1\na: 2", "1: value", "a: .inf", "a: 1\n---\nb: 2", "a: &a [*a]", "a: {<<: {b: 1}}"} {
		if _, _, err := sy.Decode([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
}
