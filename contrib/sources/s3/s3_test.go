package s3_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	x "github.com/gopherex/xconf"
	json "github.com/gopherex/xconf/contrib/decoders/json"
	source "github.com/gopherex/xconf/contrib/sources/s3"
)

func TestSDKProtocolVersionErrorsAndLimit(t *testing.T) {
	type response struct {
		status int
		body   string
	}
	var state atomic.Value
	state.Store(response{200, `{"n":9007199254740993}`})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/configs/app.json" || r.URL.Query().Get("versionId") != "v3" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.Header().Set("x-amz-version-id", "v3")
		w.Header().Set("ETag", `"etag"`)
		v := state.Load().(response)
		if v.status != 200 {
			w.Header().Set("Content-Type", "application/xml")
		}
		w.WriteHeader(v.status)
		_, _ = w.Write([]byte(v.body))
	}))
	defer server.Close()
	client := sdk.New(sdk.Options{Region: "test", BaseEndpoint: aws.String(server.URL), UsePathStyle: true, Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1})
	s := source.New(client, "configs", "app.json", json.Decode, source.Version("v3"), source.Interval(0))
	l, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.Revision != "v3" || l.Values["n"].(interface{ String() string }).String() != "9007199254740993" || l.Locations[""].Name != s.Name() {
		t.Fatal(l)
	}
	for _, code := range []string{"NoSuchKey", "NoSuchVersion", "NoSuchBucket", "AccessDenied"} {
		status := 404
		if code == "AccessDenied" {
			status = 403
		}
		state.Store(response{status, "<Error><Code>" + code + "</Code><Message>private</Message></Error>"})
		_, err = s.Read(context.Background(), nil)
		missing := code == "NoSuchKey" || code == "NoSuchVersion"
		if err == nil || errors.Is(err, x.ErrNotFound) != missing {
			t.Fatalf("%s: %v", code, err)
		}
	}
	state.Store(response{200, `{"n":2}`})
	if _, err = source.New(client, "configs", "app.json", json.Decode, source.Version("v3"), source.MaxBytes(1)).Read(context.Background(), nil); err == nil {
		t.Fatal("limit ignored")
	}
	state.Store(response{200, `not json`})
	if _, err = s.Read(context.Background(), nil); err == nil {
		t.Fatal("parse error ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Read(ctx, nil); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatal(err)
	}
}
