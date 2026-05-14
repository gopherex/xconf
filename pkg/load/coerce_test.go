package load_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gopherex/xconf"
	"github.com/gopherex/xconf/pkg/load"
)

func TestCoerceVariedInputTypes(t *testing.T) {
	type C struct {
		I   int
		I8  int8
		I16 int16
		U   uint
		U8  uint8
		F32 float32
		B   bool
		D   time.Duration
		T   time.Time
		S   string
		BS  []byte
	}
	s := xconf.Define("C",
		xconf.Int("I"), xconf.Int8("I8"), xconf.Int16("I16"),
		xconf.Uint("U"), xconf.Uint8("U8"),
		xconf.Float32("F32"),
		xconf.Bool("B"),
		xconf.Duration("D"),
		xconf.Time("T"),
		xconf.String("S"),
		xconf.Bytes("BS"),
	)
	now := time.Now().UTC().Truncate(time.Second)
	data := map[string]any{
		"I":   int64(42),
		"I8":  int8(-3),
		"I16": float64(7),       // float->int
		"U":   json.Number("9"), // json.Number path
		"U8":  uint8(8),
		"F32": int(2),  // int->float
		"B":   "true",  // string->bool
		"D":   "100ms", // string->duration
		"T":   now,     // typed time.Time
		"S":   []byte("bs"),
		"BS":  "raw",
	}
	var c C
	if err := load.Load(s, &c, load.FromMap(data)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.I != 42 || c.I8 != -3 || c.I16 != 7 || c.U != 9 || c.U8 != 8 ||
		c.F32 != 2 || !c.B || c.D != 100*time.Millisecond ||
		c.S != "bs" || string(c.BS) != "raw" {
		t.Errorf("coerce: %+v", c)
	}
	if !c.T.Equal(now) {
		t.Errorf("time: got %v want %v", c.T, now)
	}
}

func TestSliceTypedDefault(t *testing.T) {
	type C struct {
		Xs []int
	}
	s := xconf.Define("C",
		xconf.Slice[int]("Xs").Default([]int{1, 2, 3}),
	)
	var c C
	if err := load.Load(s, &c); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c.Xs) != 3 || c.Xs[2] != 3 {
		t.Errorf("typed slice default: %v", c.Xs)
	}
}

func TestMapTypedFromMapSource(t *testing.T) {
	type C struct {
		M map[string]int
	}
	s := xconf.Define("C",
		xconf.Map[string, int]("M"),
	)
	data := map[string]any{
		"M": map[string]int{"a": 1, "b": 2}, // already typed
	}
	var c C
	if err := load.Load(s, &c, load.FromMap(data)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.M["a"] != 1 || c.M["b"] != 2 {
		t.Errorf("typed map: %v", c.M)
	}
}

func TestMalformedMapEntry(t *testing.T) {
	type C struct{ M map[string]int }
	s := xconf.Define("C", xconf.Map[string, int]("M"))
	envs := map[string]string{"M": "no-separator-here"}
	var c C
	if err := load.Load(s, &c, load.FromEnv(envs)); err == nil {
		t.Errorf("expected malformed-entry error")
	}
}

func TestOptionalJSONFileReadsExisting(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/c.json"
	if err := writeFile(p, []byte(`{"X": 5}`)); err != nil {
		t.Fatal(err)
	}
	src, err := load.FromJSONFileOptional(p)
	if err != nil {
		t.Fatalf("optional existing: %v", err)
	}
	type C struct{ X int }
	s := xconf.Define("C", xconf.Int("X"))
	var c C
	if err := load.Load(s, &c, src); err != nil {
		t.Fatal(err)
	}
	if c.X != 5 {
		t.Errorf("X: %d", c.X)
	}
}

func writeFile(p string, b []byte) error {
	return os.WriteFile(p, b, 0600)
}
