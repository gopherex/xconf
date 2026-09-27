package json_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/file"
	js "github.com/gopherex/xconf/contrib/sources/json"
)

func schema() *sp.Schema {
	return sp.NewSchema(sp.ID("test", "json", sp.Ver(1, 0, 0))).Fields(sp.Int64("n").Gte(1).Required()).MustBuild()
}
func next(t *testing.T, ch <-chan x.Event, predicate func(x.Event) bool) x.Event {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatal("closed")
			}
			if predicate(e) {
				return e
			}
		case <-timer.C:
			t.Fatal("file event timeout")
		}
	}
}
func write(t *testing.T, p, data string) {
	t.Helper()
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
}
func TestFileReloadRenameMissingInvalidRecovery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	low, _ := x.NewMemory("base", map[string]any{"n": 2})
	source := x.Optional(js.File(p, file.Interval(10*time.Millisecond)))
	r, err := x.Open(context.Background(), schema(), low, source)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ch := r.Subscribe(context.Background())
	next(t, ch, func(e x.Event) bool { return e.Err == nil })
	write(t, p, `{"n":9007199254740993}`)
	e := next(t, ch, func(e x.Event) bool { return e.Err == nil && e.Snapshot.Version() > 1 })
	v, _ := x.Decode[map[string]any](e.Snapshot)
	if v["n"] != int64(9007199254740993) {
		t.Fatal(v)
	}
	version := e.Snapshot.Version()
	write(t, p, `{"n":`)
	e = next(t, ch, func(e x.Event) bool { return e.Err != nil })
	if e.Snapshot.Version() != version {
		t.Fatal("bad config published")
	}
	write(t, p, `{"n":3}`)
	next(t, ch, func(e x.Event) bool { return e.Err == nil && e.Snapshot.Version() > version })
	if err = os.Remove(p); err != nil {
		t.Fatal(err)
	}
	e = next(t, ch, func(e x.Event) bool {
		v, _ := x.Decode[map[string]any](e.Snapshot)
		return e.Err == nil && v["n"] == int64(2)
	})
	if e.Snapshot.Version() <= version {
		t.Fatal("removal did not reload")
	}
}
func TestJSONRootTrailingAndLimit(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"n":`} {
		if _, _, err := js.Decode([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	p := filepath.Join(t.TempDir(), "c.json")
	write(t, p, `{"n":1}`)
	if _, err := x.Load(context.Background(), schema(), js.File(p, file.MaxBytes(2))); err == nil {
		t.Fatal("limit ignored")
	}
}
