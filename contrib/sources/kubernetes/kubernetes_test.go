package kubernetes_test

import (
	"context"
	"encoding/json"
	"fmt"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	decoder "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/kubernetes"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typed "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func sdk(t *testing.T, server *httptest.Server) *typed.CoreV1Client {
	t.Helper()
	c, err := typed.NewForConfig(&rest.Config{Host: server.URL, BearerToken: "test-token", ContentConfig: rest.ContentConfig{ContentType: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestSDKConfigMapBindingsBinaryAndSecretDocument(t *testing.T) {
	cm := core.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "app", ResourceVersion: "7"}, Data: map[string]string{"host": "db", "empty": ""}, BinaryData: map[string][]byte{"binary": {0, 1}}}
	secret := core.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: cm.ObjectMeta, Data: map[string][]byte{"config.json": []byte(`{"n":9007199254740993}`), "password": []byte(" secret\n")}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("auth missing")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/app/configmaps/config":
			_ = json.NewEncoder(w).Encode(cm)
		case "/api/v1/namespaces/app/secrets/config":
			_ = json.NewEncoder(w).Encode(secret)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := sdk(t, server)
	l, err := source.ConfigMap(c.ConfigMaps("app"), "config", source.Bindings(map[string]x.Path{"host": {"db", "host"}, "empty": {"empty"}, "binary": {"binary"}})).Read(context.Background(), nil)
	if err != nil || l.Values["db"].(map[string]any)["host"] != "db" || l.Values["empty"] != "" || len(l.Values["binary"].([]byte)) != 2 || l.Revision != "7" {
		t.Fatal(l, err)
	}
	s := source.Secret(c.Secrets("app"), "config", source.Document("config.json", decoder.Decode))
	l, err = s.Read(context.Background(), nil)
	if err != nil || l.Values["n"].(json.Number).String() != "9007199254740993" {
		t.Fatal(l, err)
	}
	l, err = source.Secret(c.Secrets("app"), "config", source.Bindings(map[string]x.Path{"password": {"password"}})).Read(context.Background(), nil)
	if err != nil || l.Values["password"] != " secret\n" {
		t.Fatal(l, err)
	}
	for _, s := range []*source.Source{source.ConfigMap(c.ConfigMaps("app"), "config", source.MaxBytes(1)), source.Secret(c.Secrets("app"), "config", source.Document("config.json", nil)), source.ConfigMap(c.ConfigMaps("app"), "config", source.Bindings(map[string]x.Path{"host": {"a"}, "empty": {"a", "b"}}))} {
		if _, err = s.Read(context.Background(), nil); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if _, err = source.ConfigMap(c.ConfigMaps("app"), "config", source.Document("missing", decoder.Decode)).Read(context.Background(), nil); err != x.ErrNotFound {
		t.Fatal(err)
	}
}

type apiServer struct {
	t             *testing.T
	mu            sync.Mutex
	revision      int
	status        int
	raw           string
	watchers      map[chan []byte]bool
	registrations chan string
}

func newAPI(t *testing.T) *apiServer {
	return &apiServer{t: t, revision: 1, status: 404, watchers: map[chan []byte]bool{}, registrations: make(chan string, 16)}
}
func (s *apiServer) object() core.ConfigMap {
	return core.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "app", ResourceVersion: strconv.Itoa(s.revision)}, Data: map[string]string{"config.json": s.raw}}
}
func (s *apiServer) emitLocked(typ string, obj any) {
	data, _ := json.Marshal(map[string]any{"type": typ, "object": obj})
	for ch := range s.watchers {
		select {
		case ch <- data:
		default:
			s.t.Error("test watch buffer full")
		}
	}
}
func (s *apiServer) set(status int, raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revision++
	s.status = status
	s.raw = raw
	typ := "MODIFIED"
	if status == 404 {
		typ = "DELETED"
	}
	if status >= 500 {
		s.emitLocked("ERROR", metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: metav1.StatusReasonServiceUnavailable, Code: int32(status)})
		return
	}
	s.emitLocked(typ, s.object())
}
func (s *apiServer) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitLocked("ERROR", metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: metav1.StatusReasonExpired, Code: 410})
}
func (s *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/app/configmaps") {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	status := s.status
	obj := s.object()
	if status >= 500 {
		s.mu.Unlock()
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: metav1.StatusReasonServiceUnavailable, Code: int32(status)})
		return
	}
	q := r.URL.Query()
	if strings.HasSuffix(r.URL.Path, "/config") {
		s.mu.Unlock()
		if status == 404 {
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: 404})
			return
		}
		_ = json.NewEncoder(w).Encode(obj)
		return
	}
	if q.Get("fieldSelector") != "metadata.name=config" {
		s.t.Errorf("missing exact name selector: %s", r.URL)
	}
	if q.Get("watch") == "true" {
		ch := make(chan []byte, 16)
		s.watchers[ch] = true
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.watchers, ch); s.mu.Unlock() }()
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		s.registrations <- q.Get("resourceVersion")
		for {
			select {
			case <-r.Context().Done():
				return
			case data := <-ch:
				_, _ = fmt.Fprintln(w, string(data))
				w.(http.Flusher).Flush()
			}
		}
	}
	items := []core.ConfigMap{}
	if status == 200 {
		items = append(items, obj)
	}
	rv := strconv.Itoa(s.revision)
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(core.ConfigMapList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMapList"}, ListMeta: metav1.ListMeta{ResourceVersion: rv}, Items: items})
}
func TestSDKWatchMissingCreateDeleteExpiredRecoveryAndClose(t *testing.T) {
	state := newAPI(t)
	server := httptest.NewServer(state)
	defer server.Close()
	c := sdk(t, server)
	schema := sp.NewSchema(sp.ID("test", "kube", sp.Ver(1, 0, 0))).Fields(sp.Int64("n").Gte(1).Required()).MustBuild()
	low, _ := x.NewMemory("base", map[string]any{"n": 1})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := x.Open(ctx, schema, low, x.Optional(source.ConfigMap(c.ConfigMaps("app"), "config", source.Document("config.json", decoder.Decode), source.Interval(0))))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events := r.Subscribe(ctx)
	<-events
	waitRegistration := func() string {
		t.Helper()
		select {
		case rv := <-state.registrations:
			return rv
		case <-time.After(4 * time.Second):
			t.Fatal("watch registration timeout")
			return ""
		}
	}
	if rv := waitRegistration(); rv != "1" {
		t.Fatal(rv)
	}
	wait := func(pred func(x.Event) bool) x.Event {
		t.Helper()
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		for {
			select {
			case e, ok := <-events:
				if !ok {
					t.Fatal("closed")
				}
				if pred(e) {
					return e
				}
			case <-timer.C:
				t.Fatal("event timeout")
			}
		}
	}
	is := func(n int64) func(x.Event) bool {
		return func(e x.Event) bool { v, _ := x.Decode[map[string]any](e.Snapshot); return e.Err == nil && v["n"] == n }
	}
	state.set(200, `{"n":2}`)
	e := wait(is(2))
	version := e.Snapshot.Version()
	state.set(200, `{"n":0}`)
	e = wait(func(e x.Event) bool { return e.Err != nil })
	if e.Snapshot.Version() != version {
		t.Fatal("invalid config published")
	}
	state.set(404, "")
	wait(is(1))
	state.expire()
	if rv := waitRegistration(); rv != "4" {
		t.Fatal("did not relist after 410", rv)
	}
	state.set(503, "")
	wait(func(e x.Event) bool { return e.Err != nil })
	state.set(200, `{"n":3}`)
	waitRegistration()
	wait(is(3))
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = c.ConfigMaps("app").Get(ctx, "config", metav1.GetOptions{}); err != nil {
		t.Fatal("client ownership lost", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		count := len(state.watchers)
		state.mu.Unlock()
		if count == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("HTTP watch not canceled")
}
