package consul_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	source "github.com/gopherex/xconf/contrib/sources/consul"
)

func TestResilientPrefixUnreachableAtStart(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	c := client(t, server)
	server.Close() // nothing listens at the client's address any more
	schema := sp.NewSchema(sp.ID("test", "resilient", sp.Ver(1, 0, 0))).Fields(sp.Int64("n").Default(1)).MustBuild()
	s := source.NewPrefix(c.KV(), "apps/api", source.Timeout(time.Second), source.WaitTime(10*time.Millisecond))
	r, err := x.Open(context.Background(), schema, x.Resilient(s, x.Backoff(5*time.Millisecond, 20*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := x.Decode[map[string]any](r.Snapshot()); v["n"] != int64(1) || r.Snapshot().Degraded()[s.Name()] == nil {
		t.Fatal(v, r.Snapshot().Degraded())
	}
	time.Sleep(50 * time.Millisecond) // let registration and reads retry
	done := make(chan struct{})
	go func() { _ = r.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung")
	}
}
