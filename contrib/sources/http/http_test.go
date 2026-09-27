package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	json "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/http"
)

func TestConditionalCacheOwnershipAndRemoval(t *testing.T) {
	var mu sync.Mutex
	status, body, etag, expected := 200, `{"n":9007199254740993}`, `"one"`, ""
	set := func(code int, data, tag, validator string) {
		mu.Lock()
		status, body, etag, expected = code, data, tag, validator
		mu.Unlock()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if got := r.Header.Get("If-None-Match"); got != expected {
			t.Errorf("validator %q want %q", got, expected)
		}
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing configured header")
		}
		if expected != "" && r.Header.Get("If-Modified-Since") == "" {
			t.Error("missing Last-Modified validator")
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	headers := http.Header{"Authorization": {"Bearer test"}}
	s := source.New(server.Client(), server.URL+"/config?token=private", json.Decode, source.Headers(headers), source.Interval(0))
	headers.Set("Authorization", "changed")
	if strings.Contains(s.Name(), "private") {
		t.Fatal("query leaked into name")
	}
	layer, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Values["n"].(interface{ String() string }).String() != "9007199254740993" {
		t.Fatal(layer)
	}
	layer.Values["n"] = "mutated"
	set(304, "", `"one"`, `"one"`)
	next, err := s.Read(context.Background(), nil)
	if err != nil || next.Values["n"] == "mutated" {
		t.Fatal(next, err)
	}
	set(200, `{`, `"bad"`, `"one"`)
	if _, err = s.Read(context.Background(), nil); err == nil {
		t.Fatal("malformed response accepted")
	}
	set(304, "", `"one"`, `"one"`)
	if _, err = s.Read(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	set(404, "", "", `"one"`)
	if _, err = s.Read(context.Background(), nil); !errors.Is(err, x.ErrNotFound) {
		t.Fatal(err)
	}
	set(304, "", "", "")
	if _, err = s.Read(context.Background(), nil); err == nil {
		t.Fatal("304 with no cache accepted")
	}
	set(403, "private secret", "", "")
	if _, err = s.Read(context.Background(), nil); err == nil || errors.Is(err, x.ErrNotFound) || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	set(200, `{"large":"data"}`, "", "")
	headers.Set("Authorization", "Bearer test")
	if _, err = source.New(server.Client(), server.URL, json.Decode, source.Headers(headers), source.MaxBytes(2)).Read(context.Background(), nil); err == nil {
		t.Fatal("size limit ignored")
	}
}

func TestRuntimeRollbackAndPolling(t *testing.T) {
	var mu sync.Mutex
	body := `{"n":2}`
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	schema := sp.NewSchema(sp.ID("test", "http", sp.Ver(1, 0, 0))).Fields(sp.Int64("n").Gte(1).Required()).MustBuild()
	low, _ := x.NewMemory("base", map[string]any{"n": 1})
	runtime, err := x.Open(context.Background(), schema, low, x.Optional(source.New(server.Client(), server.URL, json.Decode, source.Interval(10*time.Millisecond))))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	events := runtime.Subscribe(context.Background())
	<-events
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
	mu.Lock()
	body = `{"n":0}`
	mu.Unlock()
	e := wait(func(e x.Event) bool { return e.Err != nil })
	old := e.Snapshot.Version()
	mu.Lock()
	body = `{"n":3}`
	mu.Unlock()
	wait(func(e x.Event) bool { return e.Err == nil && e.Snapshot.Version() > old })
	mu.Lock()
	status = 404
	mu.Unlock()
	e = wait(func(e x.Event) bool {
		v, _ := x.Decode[map[string]any](e.Snapshot)
		return e.Err == nil && v["n"] == int64(1)
	})
	if e.Err != nil {
		t.Fatal(e.Err)
	}
}

func TestTimeoutAndConcurrentReads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	s := source.New(server.Client(), server.URL, json.Decode, source.Timeout(30*time.Millisecond))
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Read(context.Background(), nil); err == nil {
				t.Error("timeout ignored")
			}
		}()
	}
	wg.Wait()
}
