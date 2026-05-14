package load_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gopherex/xconf"
	"github.com/gopherex/xconf/pkg/load"
)

func TestLoadRejectsNonPointer(t *testing.T) {
	type C struct{ X int }
	s := xconf.Define("C", xconf.Int("X"))
	var c C
	if err := load.Load(s, c); err == nil {
		t.Errorf("expected error for non-pointer target")
	}
}

func TestLoadRejectsNilSchema(t *testing.T) {
	var c struct{ X int }
	if err := load.Load(nil, &c); err == nil {
		t.Errorf("expected error for nil schema")
	}
}

func TestLoadRejectsMissingField(t *testing.T) {
	type C struct{ Y int } // schema says X
	s := xconf.Define("C", xconf.Int("X"))
	var c C
	envs := map[string]string{"X": "1"}
	if err := load.Load(s, &c, load.FromEnv(envs)); err == nil {
		t.Errorf("expected missing-field error")
	}
}

func TestLoadGroupTargetWrongShape(t *testing.T) {
	type C struct{ G int } // schema says G is a group
	s := xconf.Define("C", xconf.Group("G", xconf.Int("X")))
	var c C
	if err := load.Load(s, &c); err == nil {
		t.Errorf("expected group-shape error")
	}
}

func TestJSONFileMalformed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(p, []byte("{not json"), 0600)
	if _, err := load.FromJSONFile(p); err == nil {
		t.Errorf("expected json parse error")
	}
}

func TestJSONFileMissing(t *testing.T) {
	if _, err := load.FromJSONFile("/no/such/file.json"); err == nil {
		t.Errorf("expected missing-file error")
	}
}

func TestEnvCoercionFailure(t *testing.T) {
	s := xconf.Define("C", xconf.Int("X"))
	var c struct{ X int }
	envs := map[string]string{"X": "notanint"}
	if err := load.Load(s, &c, load.FromEnv(envs)); err == nil {
		t.Errorf("expected coercion error")
	}
}

func TestEnvSourceRealEnv(t *testing.T) {
	t.Setenv("XCONF_TEST_VAL", "42")
	s := xconf.Define("C", xconf.Int("XconfTestVal"))
	var c struct{ XconfTestVal int }
	if err := load.Load(s, &c, load.FromEnv(nil)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.XconfTestVal != 42 {
		t.Errorf("real env not read: %d", c.XconfTestVal)
	}
}

func TestUintAndFloat(t *testing.T) {
	type C struct {
		U uint32
		F float32
	}
	s := xconf.Define("C", xconf.Uint32("U"), xconf.Float32("F"))
	var c C
	envs := map[string]string{"U": "7", "F": "2.5"}
	if err := load.Load(s, &c, load.FromEnv(envs)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.U != 7 || c.F != 2.5 {
		t.Errorf("%+v", c)
	}
}
