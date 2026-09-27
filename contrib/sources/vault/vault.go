// Package vault reads static Vault KV v2 secrets as configuration objects.
package vault

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	decoder "github.com/gopherex/xconf/contrib/decoders/json"
	"github.com/gopherex/xconf/internal/document"
	"github.com/hashicorp/vault/api"
)

type Client interface {
	Get(context.Context, string) (*api.KVSecret, error)
	GetVersion(context.Context, string, int) (*api.KVSecret, error)
}
type Option func(*Source)
type Source struct {
	client            Client
	path, id          string
	version           int
	limit             int64
	interval, timeout time.Duration
}

func Name(id string) Option           { return func(s *Source) { s.id = id } }
func Version(n int) Option            { return func(s *Source) { s.version = n } }
func MaxBytes(n int64) Option         { return func(s *Source) { s.limit = n } }
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }
func Timeout(d time.Duration) Option  { return func(s *Source) { s.timeout = d } }

// New accepts client.KVv2(mount). Token lifecycle belongs to the caller.
// Secret data is the object itself, not a serialized document in a special key.
// Version(0) reads latest; deleted/destroyed versions count as ErrNotFound.
func New(client Client, path string, opts ...Option) *Source {
	s := &Source{client: client, path: path, id: "vault:" + path, limit: document.DefaultMaxBytes, interval: 30 * time.Second, timeout: 10 * time.Second}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if s.client == nil || s.path == "" || s.version < 0 || s.limit <= 0 || s.timeout <= 0 {
		return x.Layer{}, errors.New("invalid Vault source settings")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var secret *api.KVSecret
	var err error
	if s.version == 0 {
		secret, err = s.client.Get(ctx, s.path)
	} else {
		secret, err = s.client.GetVersion(ctx, s.path, s.version)
	}
	if errors.Is(err, api.ErrSecretNotFound) {
		return x.Layer{}, x.ErrNotFound
	}
	if err != nil {
		return x.Layer{}, err
	}
	if secret == nil {
		return x.Layer{}, errors.New("empty Vault response")
	}
	meta := secret.VersionMetadata
	if meta != nil && (meta.Destroyed || !meta.DeletionTime.IsZero()) {
		return x.Layer{}, x.ErrNotFound
	}
	if secret.Data == nil {
		return x.Layer{}, errors.New("Vault secret has no data")
	}
	data, err := json.Marshal(secret.Data)
	if err != nil {
		return x.Layer{}, err
	}
	if int64(len(data)) > s.limit {
		return x.Layer{}, errors.New("secret exceeds size limit")
	}
	if err := ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	revision := ""
	if meta != nil {
		revision = strconv.Itoa(meta.Version)
	}
	return document.Decode(data, decoder.Decode, s.id, revision)
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if s.interval == 0 {
		return func() {}, nil
	}
	return x.Poll(s, s.interval).(x.Watcher).Watch(ctx, notify)
}
