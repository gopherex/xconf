package consul_test

import (
	"context"
	"encoding/json"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	source "github.com/gopherex/xconf/contrib/sources/consul"
	"github.com/hashicorp/consul/api"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPrefixSDKPathsContainersACLAndRemoval(t *testing.T) {
	var mu sync.Mutex
	index := uint64(10)
	filtered := false
	pairs := api.KVPairs{{Key: "apps/api/db/host", Value: []byte("db"), ModifyIndex: 10}, {Key: "apps/api/names", Value: []byte(`["a","b"]`), ModifyIndex: 9}, {Key: "apps/api/folder/", Value: nil, ModifyIndex: 8}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kv/apps/api/" || !r.URL.Query().Has("recurse") {
			t.Errorf("not a prefix read: %s", r.URL)
		}
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("X-Consul-Index", fmtIndex(index))
		w.Header().Set("X-Consul-LastContact", "0")
		w.Header().Set("X-Consul-KnownLeader", "true")
		if filtered {
			w.Header().Set("X-Consul-Results-Filtered-By-ACLs", "true")
		}
		_ = json.NewEncoder(w).Encode(pairs)
	}))
	defer server.Close()
	schema := sp.NewSchema(sp.ID("test", "prefix", sp.Ver(1, 0, 0))).Fields(sp.Object("db", sp.Str("host")), sp.List("names", sp.Str(""))).MustBuild()
	s := source.NewPrefix(client(t, server).KV(), "apps/api", source.WaitTime(10*time.Millisecond))
	l, err := s.Read(context.Background(), schema)
	if err != nil || l.Values["db"].(map[string]any)["host"] != "db" || len(l.Values["names"].([]any)) != 2 {
		t.Fatal(l, err)
	}
	low, _ := x.NewMemory("low", map[string]any{"db": map[string]any{"host": "fallback"}})
	r, err := x.Open(context.Background(), schema, low, x.Optional(s))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events := r.Subscribe(context.Background())
	<-events
	mu.Lock()
	pairs = nil
	index = 11
	mu.Unlock()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-events:
			v, _ := x.Decode[map[string]any](e.Snapshot)
			if e.Err == nil && v["db"].(map[string]any)["host"] == "fallback" {
				goto recovered
			}
		case <-timer.C:
			t.Fatal("prefix removal missed")
		}
	}
recovered:
	mu.Lock()
	filtered = true
	mu.Unlock()
	if _, err = x.Optional(s).Read(context.Background(), schema); err == nil {
		t.Fatal("ACL-filtered prefix accepted")
	}
	mu.Lock()
	filtered = false
	pairs = api.KVPairs{{Key: "apps/api/db", Value: []byte(`{"host":"a"}`)}, {Key: "apps/api/db/host", Value: []byte("b")}}
	mu.Unlock()
	if _, err = s.Read(context.Background(), schema); err == nil {
		t.Fatal("parent/child conflict accepted")
	}
	mu.Lock()
	pairs = api.KVPairs{{Key: "apps/api2/wrong", Value: []byte("a")}}
	mu.Unlock()
	if _, err = s.Read(context.Background(), schema); err == nil {
		t.Fatal("prefix boundary ignored")
	}
}
func fmtIndex(v uint64) string { b, _ := json.Marshal(v); return string(b) }
