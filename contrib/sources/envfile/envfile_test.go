package envfile_test

import (
	"context"
	"errors"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	source "github.com/gopherex/xconf/contrib/sources/envfile"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBindingsConflictsMissingLimits(t *testing.T) {
	schema := sp.NewSchema(sp.ID("test", "envfile", sp.Ver(1, 0, 0))).Fields(sp.Str("password"), sp.List("hosts", sp.Str(""))).MustBuild()
	dir := t.TempDir()
	path := filepath.Join(dir, "password")
	if err := os.WriteFile(path, []byte(" secret \r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bindings := map[string]x.Path{"DB_PASSWORD_FILE": {"password"}}
	s := source.New(bindings, source.Environment([]string{"DB_PASSWORD_FILE=" + path}), source.TrimFinalNewline())
	bindings["DB_PASSWORD_FILE"][0] = "changed"
	l, err := s.Read(context.Background(), schema)
	if err != nil || l.Values["password"] != " secret " {
		t.Fatal(l, err)
	}
	if l.Locations["/password"].Name != "DB_PASSWORD_FILE:"+path {
		t.Fatal(l)
	}
	for _, opts := range [][]source.Option{
		{source.Environment([]string{"DB_PASSWORD_FILE=" + path, "DB_PASSWORD=direct"})},
		{source.Environment([]string{"DB_PASSWORD_FILE="})},
		{source.Environment([]string{"DB_PASSWORD_FILE=" + path}), source.MaxBytes(1)},
	} {
		if _, err = source.New(map[string]x.Path{"DB_PASSWORD_FILE": {"password"}}, opts...).Read(context.Background(), schema); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	missing := source.New(map[string]x.Path{"DB_PASSWORD_FILE": {"password"}}, source.Environment([]string{"DB_PASSWORD_FILE=" + path + ".missing"}))
	if _, err = x.Optional(missing).Read(context.Background(), schema); err == nil || errors.Is(err, x.ErrNotFound) {
		t.Fatal("configured missing file hidden", err)
	}
	l, err = source.New(map[string]x.Path{"DB_PASSWORD_FILE": {"password"}}, source.Environment(nil)).Read(context.Background(), schema)
	if err != nil || len(l.Values) != 0 {
		t.Fatal(l, err)
	}
	if err = os.WriteFile(path, []byte(`["a","b"]`), 0600); err != nil {
		t.Fatal(err)
	}
	l, err = source.New(map[string]x.Path{"HOSTS_FILE": {"hosts"}}, source.Environment([]string{"HOSTS_FILE=" + path})).Read(context.Background(), schema)
	if err != nil || len(l.Values["hosts"].([]any)) != 2 {
		t.Fatal(l, err)
	}
}
func TestAtomicReplacementRuntimeAndRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	schema := sp.NewSchema(sp.ID("test", "file", sp.Ver(1, 0, 0))).Fields(sp.Str("password").Required()).MustBuild()
	r, err := x.Open(context.Background(), schema, source.New(map[string]x.Path{"PASSWORD_FILE": {"password"}}, source.Environment([]string{"PASSWORD_FILE=" + path}), source.Interval(10*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events := r.Subscribe(context.Background())
	<-events
	if err = os.WriteFile(path+".new", []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	wait := func(pred func(x.Event) bool) x.Event {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case e := <-events:
				if pred(e) {
					return e
				}
			case <-timer.C:
				t.Fatal("timeout")
			}
		}
	}
	e := wait(func(e x.Event) bool {
		v, _ := x.Decode[map[string]any](e.Snapshot)
		return e.Err == nil && v["password"] == "second"
	})
	version := e.Snapshot.Version()
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	e = wait(func(e x.Event) bool { return e.Err != nil })
	if e.Snapshot.Version() != version {
		t.Fatal("missing file replaced snapshot")
	}
}
