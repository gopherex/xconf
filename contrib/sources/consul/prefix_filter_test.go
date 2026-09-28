package consul_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	source "github.com/gopherex/xconf/contrib/sources/consul"
	"github.com/hashicorp/consul/api"
)

func TestPrefixIgnoreKeysSDKRevisionAndRemoval(t *testing.T) {
	var mu sync.Mutex
	index := uint64(10)
	pairs := api.KVPairs{
		{Key: "apps/api/_revision", Value: []byte("10")},
		{Key: "apps/api/_meta/nested/value", Value: []byte("not config")},
		{Key: "apps/api/config_revision", Value: []byte("10")},
		{Key: "apps/api/db/_note/nested", Value: []byte("not config")},
		{Key: "apps/api/db/host", Value: []byte("db"), ModifyIndex: 8},
		{Key: "apps/api/host", Value: []byte("api"), ModifyIndex: 9},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kv/apps/api/" || !r.URL.Query().Has("recurse") {
			t.Errorf("not a prefix read: %s", r.URL)
		}
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("X-Consul-Index", fmtIndex(index))
		w.Header().Set("X-Consul-LastContact", "0")
		_ = json.NewEncoder(w).Encode(pairs)
	}))
	defer server.Close()
	kv := client(t, server).KV()
	schema := sp.NewSchema(sp.ID("test", "ignore", sp.Ver(1, 0, 0))).Strict().Fields(sp.Str("host"), sp.Object("db", sp.Str("host"))).MustBuild()
	ctx := context.Background()
	if _, err := x.Load(ctx, schema, source.NewPrefix(kv, "apps/api")); err == nil {
		t.Fatal("service keys must still fail without an explicit filter")
	}
	patterns := []string{"_*", "db/_*"}
	ignore := source.IgnoreKeys(patterns...)
	patterns[0] = "*" // The option must own its patterns before being applied.
	s := source.NewPrefix(kv, "apps/api", ignore, source.IgnoreKeys("config_revision"), source.WaitTime(0))
	layer, err := s.Read(ctx, schema)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"host": "api", "db": map[string]any{"host": "db"}}
	if !reflect.DeepEqual(layer.Values, want) || len(layer.Locations) != 2 {
		t.Fatalf("unexpected filtered layer: %+v", layer)
	}
	low, _ := x.NewMemory("fallback", map[string]any{"host": "fallback"})
	runtime, err := x.Open(ctx, schema, low, x.Optional(s))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	initial := runtime.Snapshot()
	if _, exists := initial.Origins()["/_revision"]; exists {
		t.Fatal("ignored key has provenance")
	}
	mu.Lock()
	pairs[0].Value = []byte("changed")
	pairs[0].ModifyIndex = 11
	index = 11
	mu.Unlock()
	after, err := runtime.Reload(ctx)
	if err != nil || after.Version() != initial.Version() || after.Revisions()[s.Name()] != layer.Revision {
		t.Fatalf("ignored update changed the snapshot: %v", err)
	}
	mu.Lock()
	pairs[5].Value = []byte("updated")
	pairs[5].ModifyIndex = 12
	index = 12
	mu.Unlock()
	after, err = runtime.Reload(ctx)
	if err != nil || after.Version() != initial.Version()+1 {
		t.Fatalf("config update not published: %v", err)
	}
	mu.Lock()
	pairs = pairs[:4] // Only ignored keys remain.
	index = 13
	mu.Unlock()
	if _, err := s.Read(ctx, schema); !errors.Is(err, x.ErrNotFound) {
		t.Fatalf("only ignored keys: %v", err)
	}
	after, err = runtime.Reload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	value, err := x.Decode[map[string]any](after)
	if err != nil || value["host"] != "fallback" {
		t.Fatalf("lower layer not restored: %v, %v", value, err)
	}
}

type listFunc func(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)

func (f listFunc) List(key string, options *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
	return f(key, options)
}

func TestPrefixIgnoreKeysKeepsSafetyChecks(t *testing.T) {
	schema := sp.NewSchema(sp.ID("test", "ignore", sp.Ver(1, 0, 0))).Strict().Fields(sp.Str("host")).MustBuild()
	for _, test := range []struct {
		name     string
		pairs    api.KVPairs
		filtered bool
	}{
		{"outside prefix", api.KVPairs{{Key: "apps/other/_ignored"}}, false},
		{"unmatched unknown key", api.KVPairs{{Key: "apps/api/unknown"}}, false},
		{"ACL filtered", api.KVPairs{{Key: "apps/api/_ignored"}}, true},
		{"ignored oversized value", api.KVPairs{{Key: "apps/api/_ignored", Value: []byte("12345")}}, false},
		{"too many ignored keys", make(api.KVPairs, 1025), false},
		{"nil pair", api.KVPairs{nil}, false},
		{"duplicate config keys", api.KVPairs{{Key: "apps/api/host"}, {Key: "apps/api/host"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			kv := listFunc(func(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
				return test.pairs, &api.QueryMeta{LastIndex: 1, ResultsFilteredByACLs: test.filtered}, nil
			})
			s := source.NewPrefix(kv, "apps/api", source.IgnoreKeys("_*"), source.MaxBytes(4))
			if _, err := x.Load(context.Background(), schema, x.Optional(s)); err == nil || errors.Is(err, x.ErrNotFound) {
				t.Fatalf("filter hid an error: %v", err)
			}
		})
	}
}

func TestIgnoreKeysRejectsInvalidSettingsBeforeIO(t *testing.T) {
	ctx := context.Background()
	schema := sp.NewSchema(sp.ID("test", "ignore", sp.Ver(1, 0, 0))).MustBuild()
	kv := listFunc(func(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
		t.Fatal("invalid filter reached client")
		return nil, nil, nil
	})
	for _, pattern := range []string{"[", "ok/[", "\\"} {
		s := source.NewPrefix(kv, "apps/api", source.IgnoreKeys(pattern), source.WaitTime(0))
		if _, err := s.Read(ctx, schema); err == nil {
			t.Fatalf("Read accepted invalid pattern %q", pattern)
		}
		if stop, err := s.Watch(ctx, func() {}); err == nil {
			stop()
			t.Fatalf("Watch accepted invalid pattern %q", pattern)
		}
	}
	exact := source.New(getFunc(func(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
		t.Fatal("prefix-only option reached exact-key client")
		return nil, nil, nil
	}), "apps/api", func([]byte) (map[string]any, map[string]x.Location, error) {
		return map[string]any{}, nil, nil
	}, source.IgnoreKeys("_*"), source.WaitTime(0))
	if _, err := exact.Read(ctx, schema); err == nil {
		t.Fatal("IgnoreKeys silently accepted by exact-key source")
	}
	if stop, err := exact.Watch(ctx, func() {}); err == nil {
		stop()
		t.Fatal("Watch silently accepted IgnoreKeys on exact-key source")
	}
}
