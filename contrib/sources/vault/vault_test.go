package vault_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	x "github.com/gopherex/xconf"
	source "github.com/gopherex/xconf/contrib/sources/vault"
	"github.com/hashicorp/vault/api"
)

func TestSDKProtocolExactNumbersAndDeletedVersions(t *testing.T) {
	type response struct {
		status int
		body   string
	}
	var state atomic.Value
	state.Store(response{200, `{"data":{"data":{"n":9007199254740993},"metadata":{"version":3,"destroyed":false,"deletion_time":""}}}`})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/apps/api" || r.URL.Query().Get("version") != "3" || r.Header.Get("X-Vault-Token") != "test" {
			t.Errorf("unexpected request %s", r.URL)
		}
		v := state.Load().(response)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(v.status)
		_, _ = w.Write([]byte(v.body))
	}))
	defer server.Close()
	config := api.DefaultConfig()
	config.Address = server.URL
	config.HttpClient = server.Client()
	config.MaxRetries = 0
	client, err := api.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	client.SetToken("test")
	s := source.New(client.KVv2("secret"), "apps/api", source.Version(3), source.Interval(0))
	l, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.Revision != "3" || l.Values["n"].(interface{ String() string }).String() != "9007199254740993" {
		t.Fatal(l)
	}
	for _, v := range []response{
		{404, `{"errors":[]}`},
		{404, `{"data":{"data":null,"metadata":{"version":3,"destroyed":false,"deletion_time":"2026-01-01T00:00:00Z"}}}`},
		{404, `{"data":{"data":null,"metadata":{"version":3,"destroyed":true,"deletion_time":""}}}`},
	} {
		state.Store(v)
		if _, err = s.Read(context.Background(), nil); !errors.Is(err, x.ErrNotFound) {
			t.Fatalf("%s: %v", v.body, err)
		}
	}
	state.Store(response{403, `{"errors":["permission denied"]}`})
	if _, err = x.Optional(s).Read(context.Background(), nil); err == nil || errors.Is(err, x.ErrNotFound) {
		t.Fatal(err)
	}
	state.Store(response{200, `{"data":{"data":{"n":1},"metadata":{"version":3}}}`})
	if _, err = source.New(client.KVv2("secret"), "apps/api", source.Version(3), source.MaxBytes(1)).Read(context.Background(), nil); err == nil {
		t.Fatal("limit ignored")
	}
	state.Store(response{200, `{"data":{"data":null,"metadata":{"version":3}}}`})
	if _, err = s.Read(context.Background(), nil); err == nil || errors.Is(err, x.ErrNotFound) {
		t.Fatal("malformed response hidden", err)
	}
}
