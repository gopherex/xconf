package directory_test

import (
	"context"
	"errors"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	source "github.com/gopherex/xconf/contrib/sources/directory"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestBindingsWhitespaceRemovalLimits(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "password", " secret \r\n")
	write(t, dir, "empty", "")
	bindings := map[string]x.Path{"password": {"db", "password"}, "empty": {"empty"}}
	s := source.New(dir, bindings, source.TrimFinalNewline())
	bindings["password"][0] = "changed"
	l, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.Values["db"].(map[string]any)["password"] != " secret " || l.Values["empty"] != "" {
		t.Fatal(l)
	}
	if l.Locations["/db/password"].Name != filepath.Join(dir, "password") {
		t.Fatal(l)
	}
	if err = os.Remove(filepath.Join(dir, "password")); err != nil {
		t.Fatal(err)
	}
	l, err = s.Read(context.Background(), nil)
	if err != nil || l.Values["db"] != nil {
		t.Fatal(l, err)
	}
	write(t, dir, "large", "1234")
	for _, s := range []*source.Source{source.New(dir, nil, source.MaxBytes(2)), source.New(dir, map[string]x.Path{"empty": {"a"}, "large": {"a", "b"}}), source.New(dir, map[string]x.Path{"../outside": {"x"}})} {
		if _, err = s.Read(context.Background(), nil); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	if _, err = source.New(filepath.Join(dir, "absent"), nil).Read(context.Background(), nil); !errors.Is(err, x.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestProjectedGenerationAndReload(t *testing.T) {
	dir := t.TempDir()
	for _, gen := range []string{"..one", "..two"} {
		if err := os.Mkdir(filepath.Join(dir, gen), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "..one"), "password", "first")
	write(t, filepath.Join(dir, "..two"), "password", "second")
	if err := os.Symlink("..one", filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..data/password", filepath.Join(dir, "password")); err != nil {
		t.Fatal(err)
	}
	schema := sp.NewSchema(sp.ID("test", "directory", sp.Ver(1, 0, 0))).Fields(sp.Str("password").Required()).MustBuild()
	r, err := x.Open(context.Background(), schema, source.New(dir, nil, source.Interval(10*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events := r.Subscribe(context.Background())
	<-events
	if err = os.Symlink("..two", filepath.Join(dir, "..next")); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(dir, "..next"), filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-events:
			if e.Err != nil {
				t.Fatal(e.Err)
			}
			v, _ := x.Decode[map[string]any](e.Snapshot)
			if v["password"] == "second" {
				return
			}
		case <-timer.C:
			t.Fatal("reload timeout")
		}
	}
}
