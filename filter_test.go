package xconf_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
)

func TestAllowPathsLayeringPresenceAndOwnership(t *testing.T) {
	s := sp.NewSchema(sp.ID("test", "filtered", sp.Ver(1, 0, 0))).Strict().Fields(
		sp.Object("server", sp.Int64("limit"), sp.Str("host")), sp.Bool("on"), sp.Str("text"),
		sp.Str("nullable").Nullable(), sp.List("list", sp.Str("")), sp.MapOf("labels", sp.Str("value")),
	).MustBuild()
	low := memory(t, "file", map[string]any{"server": map[string]any{"limit": 9, "host": "local"}})
	raw := x.Layer{
		Values: map[string]any{
			"server": map[string]any{"limit": int64(0), "host": "remote"},
			"on":     false, "text": "", "nullable": nil, "list": []any{},
			"labels":  map[string]any{"a/b~c.d": "kept", "a": "dropped"},
			"unknown": "ignored before strict validation",
		},
		Revision: "remote-17",
		Locations: map[string]x.Location{
			"": {Name: "root"}, "/server": {Name: "parent", Line: 2},
			"/server/host": {Name: "excluded"}, "/labels/a~1b~0c.d": {Name: "literal"},
		},
	}
	source := x.SourceFunc{ID: "remote", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) { return raw, nil }}
	path := x.Path{"server", "limit"}
	filtered := x.AllowPaths(source, path, path, x.Path{"on"}, x.Path{"text"}, x.Path{"nullable"}, x.Path{"list"}, x.Path{"labels", "a/b~c.d"})
	path[1] = "host" // The wrapper owns the selection.
	got := load(t, s, low, filtered)
	want := map[string]any{
		"server": map[string]any{"limit": int64(0), "host": "local"},
		"on":     false, "text": "", "nullable": nil, "list": []any{},
		"labels": map[string]any{"a/b~c.d": "kept"},
	}
	if actual := value(t, got); !reflect.DeepEqual(actual, want) {
		t.Fatalf("got %#v, want %#v", actual, want)
	}
	history := got.Explain("server", "limit")
	if len(history) != 2 || history[1].Source != source.Name() || history[1].Revision != raw.Revision || history[1].Location.Name != "parent" {
		t.Fatalf("provenance lost: %+v", history)
	}
	if len(got.Explain("server", "host")) != 1 || len(got.Explain("unknown")) != 0 {
		t.Fatal("excluded paths acquired provenance")
	}
	layer, err := filtered.Read(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := layer.Locations["/server/host"]; exists {
		t.Fatal("excluded location leaked")
	}
	layer.Values["server"].(map[string]any)["limit"] = 100
	delete(layer.Locations, "/server")
	if raw.Values["server"].(map[string]any)["limit"] != int64(0) || raw.Locations["/server"].Name != "parent" {
		t.Fatal("returned layer aliases the source")
	}
}

func TestAllowPathsEmptyRootSubtreeAndAncestors(t *testing.T) {
	ctx := context.Background()
	s := schema(sp.Object("server", sp.Int64("limit")), sp.MapOf("labels", sp.Str("value")))
	for _, tc := range []struct {
		name  string
		paths []x.Path
		input map[string]any
		want  map[string]any
	}{
		{"deny all", nil, map[string]any{"server": nil}, map[string]any{}},
		{"root", []x.Path{{}}, map[string]any{"server": nil}, map[string]any{"server": nil}},
		{"empty object selected", []x.Path{{"labels"}}, map[string]any{"labels": map[string]any{}}, map[string]any{"labels": map[string]any{}}},
		{"absent child", []x.Path{{"server", "limit"}}, map[string]any{"server": map[string]any{}}, map[string]any{}},
		{"null parent", []x.Path{{"server", "limit"}}, map[string]any{"server": nil}, map[string]any{}},
		{"scalar parent", []x.Path{{"server", "limit"}}, map[string]any{"server": "closed"}, map[string]any{}},
		{"parent subsumes child", []x.Path{{"labels", "a"}, {"labels"}}, map[string]any{"labels": map[string]any{"a": "a", "b": "b"}}, map[string]any{"labels": map[string]any{"a": "a", "b": "b"}}},
		{"numeric map key", []x.Path{{"labels", "0"}}, map[string]any{"labels": map[string]any{"0": "a", "1": "b"}}, map[string]any{"labels": map[string]any{"0": "a"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layer, err := x.AllowPaths(memory(t, "source", tc.input), tc.paths...).Read(ctx, s)
			if err != nil || !reflect.DeepEqual(layer.Values, tc.want) {
				t.Fatalf("got %+v, %v; want %+v", layer.Values, err, tc.want)
			}
		})
	}
}

func TestAllowPathsProjectsAncestorEdits(t *testing.T) {
	s := schema(sp.Object("server", sp.Int64("limit").Default(7), sp.Str("host")), sp.MapOf("labels", sp.Str("value")))
	for _, tc := range []struct {
		name  string
		edits []x.Edit
		limit int64
	}{
		{"replace ancestor", []x.Edit{{Kind: x.Replace, Path: x.Path{"server"}, Value: map[string]any{"host": "remote", "limit": 4}}}, 4},
		{"clear ancestor", []x.Edit{{Kind: x.Replace, Path: x.Path{"server"}, Value: map[string]any{}}}, 7},
		{"null ancestor", []x.Edit{{Kind: x.Replace, Path: x.Path{"server"}, Value: nil}}, 7},
		{"delete ancestor", []x.Edit{{Kind: x.Delete, Path: x.Path{"server"}}}, 7},
		{"excluded edit", []x.Edit{{Kind: x.Replace, Path: x.Path{"server", "host"}, Value: "remote"}}, 9},
		{"ordered edits", []x.Edit{{Kind: x.Delete, Path: x.Path{"server"}}, {Kind: x.Replace, Path: x.Path{"server", "limit"}, Value: 12}}, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			low := memory(t, "file", map[string]any{"server": map[string]any{"limit": 1, "host": "local"}, "labels": map[string]any{"a": "old", "b": "kept"}})
			high := memory(t, "remote", nil)
			edits := append(tc.edits, x.Edit{Kind: x.Replace, Path: x.Path{"labels", "a"}, Value: "new"})
			if err := high.Set(x.Layer{Values: map[string]any{"server": map[string]any{"limit": 9}}, Edits: edits, Locations: map[string]x.Location{"/server": {Name: "parent edit"}}}); err != nil {
				t.Fatal(err)
			}
			filtered := x.AllowPaths(high, x.Path{"server", "limit"}, x.Path{"labels"})
			got := load(t, s, low, filtered)
			want := map[string]any{"server": map[string]any{"limit": tc.limit, "host": "local"}, "labels": map[string]any{"a": "new", "b": "kept"}}
			if actual := value(t, got); !reflect.DeepEqual(actual, want) {
				t.Fatalf("got %#v, want %#v", actual, want)
			}
			if h := got.Explain("server", "host"); len(h) != 1 || h[0].Source != "file" {
				t.Fatalf("ancestor edit touched excluded sibling: %+v", h)
			}
			layer, err := filtered.Read(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			for _, edit := range layer.Edits {
				if edit.Path.String() == "/server" {
					t.Fatal("unfiltered ancestor edit")
				}
			}
		})
	}
}

func TestAllowPathsListsAndDiscriminators(t *testing.T) {
	union := sp.OneOf("job", "kind").Variant("a", sp.Str("kind"), sp.Str("live"), sp.Str("fixed")).Variant("b", sp.Str("kind"), sp.Str("live"))
	s := schema(sp.List("list", sp.Str("")), union, sp.MapOf("jobs", union))
	for _, path := range []x.Path{{"list", "0"}, {"job", "kind"}, {"jobs", "a", "kind"}} {
		_, err := x.AllowPaths(memory(t, "remote", nil), path).Read(context.Background(), s)
		if err == nil || !strings.Contains(err.Error(), "select the whole") {
			t.Fatalf("unsafe selection %v accepted: %v", path, err)
		}
	}
	low := memory(t, "file", map[string]any{"job": map[string]any{"kind": "a", "live": "old", "fixed": "local"}})
	high := memory(t, "remote", map[string]any{"job": map[string]any{"kind": "b", "live": "new"}})
	got := value(t, load(t, s, low, x.AllowPaths(high, x.Path{"job", "live"})))
	if !reflect.DeepEqual(got["job"], map[string]any{"kind": "a", "live": "new", "fixed": "local"}) {
		t.Fatalf("partial selection switched the variant: %+v", got)
	}
	got = value(t, load(t, s, low, x.AllowPaths(high, x.Path{"job"})))
	if !reflect.DeepEqual(got["job"], map[string]any{"kind": "b", "live": "new"}) {
		t.Fatalf("whole-object selection did not switch the variant: %+v", got)
	}
}

func TestAllowPathsWatchRollbackAndRemoval(t *testing.T) {
	s := schema(sp.Object("server", sp.Int64("limit").Gte(1), sp.Str("host")))
	low := memory(t, "file", map[string]any{"server": map[string]any{"limit": 2, "host": "local"}})
	high := memory(t, "remote", map[string]any{"server": map[string]any{"limit": 3, "host": "ignored"}})
	r, err := x.Open(context.Background(), s, low, x.AllowPaths(high, x.Path{"server", "limit"}))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events := r.Subscribe(context.Background())
	initial := event(t, events)
	for _, tc := range []struct {
		values map[string]any
		limit  int64
		fail   bool
	}{
		{map[string]any{"server": map[string]any{"limit": 0, "host": "ignored"}}, 3, true},
		{map[string]any{"server": map[string]any{"limit": 4, "host": "ignored"}}, 4, false},
		{map[string]any{"server": map[string]any{"host": "ignored"}}, 2, false},
	} {
		if err := high.Set(x.Layer{Values: tc.values}); err != nil {
			t.Fatal(err)
		}
		got := event(t, events)
		if (got.Err != nil) != tc.fail || (tc.fail && got.Snapshot.Version() != initial.Snapshot.Version()) {
			t.Fatalf("unexpected event: %+v", got)
		}
		server := value(t, got.Snapshot)["server"].(map[string]any)
		if server["limit"] != tc.limit || server["host"] != "local" {
			t.Fatalf("wrong live values: %+v", server)
		}
	}
}

func TestAllowPathsErrorsAndStaticWatch(t *testing.T) {
	ctx := context.Background()
	for _, cause := range []error{x.ErrNotFound, context.Canceled, errors.New("provider failed")} {
		source := x.SourceFunc{ID: "source", ReadFunc: func(context.Context, *sp.Schema) (x.Layer, error) { return x.Layer{}, cause }}
		filtered := x.AllowPaths(source) // Deny-all does not suppress source errors.
		if _, err := filtered.Read(ctx, schema()); !errors.Is(err, cause) {
			t.Fatalf("error lost: %v", err)
		}
		stop, err := filtered.(x.Watcher).Watch(ctx, func() { t.Error("static source notified") })
		if err != nil {
			t.Fatal(err)
		}
		stop()
	}
}
