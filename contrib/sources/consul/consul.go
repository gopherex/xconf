// Package consul reads Consul KV documents or field prefixes and watches their index.
package consul

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/env"
	"github.com/gopherex/xconf/internal/document"
	"github.com/hashicorp/consul/api"
	"golang.org/x/time/rate"
)

// Client is implemented by *api.KV. Implementations must honor QueryOptions.Context.
type Client interface {
	Get(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)
}

// ListClient is implemented by *api.KV.
type ListClient interface {
	List(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)
}

type Option func(*Source)
type Source struct {
	client        Client
	listClient    ListClient
	key, id       string
	decode        x.Decoder
	ignoreKeys    []string
	limit         int64
	timeout, wait time.Duration
}

func Name(id string) Option   { return func(s *Source) { s.id = id } }
func MaxBytes(n int64) Option { return func(s *Source) { s.limit = n } }

// IgnoreKeys excludes keys from NewPrefix using path.Match patterns relative to
// the selected prefix. A matching parent excludes its whole subtree: "_*" skips
// top-level underscore keys and their descendants; "db/_*" skips them under db.
// Multiple options accumulate. Patterns are copied when the option is created.
// Invalid patterns or using this option with New cause Read and Watch to fail.
// Response size/count limits and ACL checks still apply to the entire response.
func IgnoreKeys(patterns ...string) Option {
	patterns = append([]string(nil), patterns...)
	return func(s *Source) { s.ignoreKeys = append(s.ignoreKeys, patterns...) }
}

// Timeout bounds ordinary reads and adds a network allowance to blocking queries.
func Timeout(d time.Duration) Option { return func(s *Source) { s.timeout = d } }

// WaitTime sets the server-side blocking duration, at most ten minutes.
// Zero disables watching. The HTTP client timeout must allow this wait plus jitter.
func WaitTime(d time.Duration) Option { return func(s *Source) { s.wait = d } }

// New accepts client.KV() from a caller-configured Consul API client. Auth, TLS,
// datacenter, namespace and partition belong to that client. One exact key holds
// one complete document; prefixes are not expanded. The client is never closed.
func New(client Client, key string, decode x.Decoder, opts ...Option) *Source {
	s := &Source{client: client, key: key, id: "consul:" + key, decode: decode,
		limit: document.DefaultMaxBytes, timeout: 10 * time.Second, wait: 5 * time.Minute}
	for _, o := range opts {
		o(s)
	}
	return s
}

// NewPrefix maps slash-separated keys below prefix to schema paths. Values use
// env encoding: strings for scalars, JSON for containers. Empty folder markers
// are ignored. The entire prefix is read in one query; ACL-filtered views fail.
func NewPrefix(client ListClient, prefix string, opts ...Option) *Source {
	s := New(nil, strings.TrimSuffix(prefix, "/")+"/", nil, opts...)
	s.listClient = client
	if s.id == "consul:"+s.key {
		s.id = "consul:prefix:" + s.key
	}
	return s
}

func (s *Source) Name() string { return s.id }
func (s *Source) valid() bool {
	if len(s.ignoreKeys) > 0 && s.listClient == nil {
		return false
	}
	for _, pattern := range s.ignoreKeys {
		if _, err := path.Match(pattern, ""); err != nil {
			return false
		}
	}
	return (s.client != nil && s.decode != nil || s.listClient != nil) && s.key != "" && !strings.HasPrefix(s.key, "/") &&
		s.limit > 0 && s.timeout > 0 && s.wait >= 0 &&
		s.wait <= 10*time.Minute && s.timeout <= time.Duration(math.MaxInt64)-s.wait-s.wait/16
}

func (s *Source) get(ctx context.Context, index uint64, wait time.Duration) (api.KVPairs, *api.QueryMeta, error) {
	// Consul may add up to wait/16 jitter before returning a blocking request.
	ctx, cancel := context.WithTimeout(ctx, wait+wait/16+s.timeout)
	defer cancel()
	q := (&api.QueryOptions{WaitIndex: index, WaitTime: wait, RequireConsistent: true}).WithContext(ctx)
	var pairs api.KVPairs
	var meta *api.QueryMeta
	var err error
	if s.listClient != nil {
		pairs, meta, err = s.listClient.List(s.key, q)
	} else {
		var pair *api.KVPair
		pair, meta, err = s.client.Get(s.key, q)
		if pair != nil {
			pairs = api.KVPairs{pair}
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if meta == nil {
		return nil, nil, errors.New("missing Consul query metadata")
	}
	if meta.ResultsFilteredByACLs {
		return nil, nil, errors.New("Consul results filtered by ACLs")
	}
	if len(pairs) > 1024 {
		return nil, nil, errors.New("too many Consul keys")
	}
	remaining := s.limit
	for _, pair := range pairs {
		if pair == nil {
			return nil, nil, errors.New("nil Consul KV entry")
		}
		remaining -= int64(len(pair.Value))
		if remaining < 0 {
			return nil, nil, errors.New("document exceeds size limit")
		}
	}
	return pairs, meta, nil
}

func (s *Source) Read(ctx context.Context, schema *sp.Schema) (x.Layer, error) {
	if !s.valid() {
		return x.Layer{}, errors.New("invalid Consul source settings")
	}
	pairs, _, err := s.get(ctx, 0, 0)
	if err != nil {
		return x.Layer{}, err
	}
	// The official SDK represents HTTP 404 as a nil pair with query metadata.
	if len(pairs) == 0 {
		return x.Layer{}, x.ErrNotFound
	}
	if s.listClient != nil {
		return s.decodePrefix(schema, pairs)
	}
	pair := pairs[0]
	return document.Decode(pair.Value, s.decode, s.id, strconv.FormatUint(pair.ModifyIndex, 10))
}

func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if !s.valid() {
		return nil, errors.New("invalid Consul source settings")
	}
	if s.wait == 0 {
		return func() {}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	// Capture a baseline before Open's initial Read. Changes in the gap are
	// observed by the next query, even when the key does not exist yet.
	_, meta, err := s.get(ctx, 0, 0)
	if err != nil {
		cancel()
		return nil, err
	}
	index := max(meta.LastIndex, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Permit quick initial changes, then bound churn/buggy zero-index replies.
		limiter := rate.NewLimiter(rate.Every(time.Second), 2)
		backoff := 250 * time.Millisecond
		failed := false
		for {
			if err := limiter.Wait(ctx); err != nil {
				return
			}
			_, meta, err := s.get(ctx, index, s.wait)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				failed = true
				notify()
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				backoff = min(backoff*2, 5*time.Second)
				continue
			}
			if failed || meta.LastIndex != index {
				notify()
			}
			failed = false
			backoff = 250 * time.Millisecond
			next := max(meta.LastIndex, 1)
			if next < index {
				// A restored snapshot may move the index backwards. Restart the
				// query rather than waiting for an old, possibly unreachable index.
				index = 0
			} else {
				index = next
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

func (s *Source) decodePrefix(schema *sp.Schema, pairs api.KVPairs) (x.Layer, error) {
	// Do not reorder a slice owned by the injected SDK client.
	pairs = append(api.KVPairs(nil), pairs...)
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
	vars := map[string]string{}
	opts := []env.Option{env.Prefix("__XCONF_CONSUL_")}
	locations := map[string]x.Location{}
	hash := sha256.New()
	for _, pair := range pairs {
		if !strings.HasPrefix(pair.Key, s.key) {
			return x.Layer{}, errors.New("Consul returned key outside prefix")
		}
		suffix := strings.TrimPrefix(pair.Key, s.key)
		if s.ignored(suffix) {
			continue
		}
		if strings.HasSuffix(pair.Key, "/") && len(pair.Value) == 0 {
			continue
		}
		path := x.Path(strings.Split(suffix, "/"))
		for _, part := range path {
			if part == "" {
				return x.Layer{}, errors.New("empty Consul path segment")
			}
		}
		if _, exists := vars[pair.Key]; exists {
			return x.Layer{}, errors.New("duplicate Consul key")
		}
		vars[pair.Key] = string(pair.Value)
		opts = append(opts, env.Bind(pair.Key, path...))
		locations[path.String()] = x.Location{Name: s.id + ":" + pair.Key}
		fmt.Fprintf(hash, "%d:%s:%d:%d:", len(pair.Key), pair.Key, pair.ModifyIndex, len(pair.Value))
		hash.Write(pair.Value)
	}
	if len(vars) == 0 {
		return x.Layer{}, x.ErrNotFound
	}
	layer, err := env.Decode(schema, vars, opts...)
	if err != nil {
		return x.Layer{}, err
	}
	layer.Locations = locations
	layer.Revision = fmt.Sprintf("%x", hash.Sum(nil))
	return layer, nil
}

func (s *Source) ignored(key string) bool {
	if len(s.ignoreKeys) == 0 {
		return false
	}
	for {
		for _, pattern := range s.ignoreKeys {
			if match, _ := path.Match(pattern, key); match {
				return true
			}
		}
		index := strings.LastIndexByte(key, '/')
		if index < 0 {
			return false
		}
		key = key[:index]
	}
}
