// Package kubernetes watches named ConfigMaps and Secrets through the Kubernetes API.
package kubernetes

import (
	"context"
	"errors"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"strings"
	"time"
)

type ConfigMapClient interface {
	Get(context.Context, string, metav1.GetOptions) (*core.ConfigMap, error)
	List(context.Context, metav1.ListOptions) (*core.ConfigMapList, error)
	Watch(context.Context, metav1.ListOptions) (watch.Interface, error)
}
type SecretClient interface {
	Get(context.Context, string, metav1.GetOptions) (*core.Secret, error)
	List(context.Context, metav1.ListOptions) (*core.SecretList, error)
	Watch(context.Context, metav1.ListOptions) (watch.Interface, error)
}
type Option func(*Source)
type Source struct {
	name, id, key     string
	decode            x.Decoder
	bindings          map[string]x.Path
	timeout, interval time.Duration
	limit             int64
	get               func(context.Context) (map[string]any, string, error)
	list              func(context.Context, metav1.ListOptions) (string, error)
	watch             func(context.Context, metav1.ListOptions) (watch.Interface, error)
}

func Name(id string) Option          { return func(s *Source) { s.id = id } }
func MaxBytes(n int64) Option        { return func(s *Source) { s.limit = n } }
func Timeout(d time.Duration) Option { return func(s *Source) { s.timeout = d } }

// Interval configures reconciliation in addition to watch; zero disables it.
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }

// Document reads one data key as a complete document. It cannot be combined with Bindings.
func Document(key string, decode x.Decoder) Option {
	return func(s *Source) { s.key = key; s.decode = decode }
}

// Bindings selects keys and maps them to schema paths. Missing selected keys are
// absent values. Nil selects all literal top-level keys; an empty map selects none.
func Bindings(bindings map[string]x.Path) Option {
	return func(s *Source) {
		if bindings == nil {
			s.bindings = nil
			return
		}
		s.bindings = map[string]x.Path{}
		for k, p := range bindings {
			s.bindings[k] = append(x.Path(nil), p...)
		}
	}
}
func newSource(kind, name string, opts []Option) *Source {
	s := &Source{name: name, id: "kubernetes:" + kind + ":" + name, timeout: 10 * time.Second, interval: 30 * time.Second, limit: document.DefaultMaxBytes}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ConfigMap accepts a namespace-scoped client, e.g. coreClient.ConfigMaps("app").
func ConfigMap(client ConfigMapClient, name string, opts ...Option) *Source {
	s := newSource("configmap", name, opts)
	if client == nil {
		return s
	}
	s.get = func(ctx context.Context) (map[string]any, string, error) {
		cm, err := client.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		if cm == nil {
			return nil, "", errors.New("nil ConfigMap")
		}
		values := map[string]any{}
		for k, v := range cm.Data {
			values[k] = v
		}
		for k, v := range cm.BinaryData {
			if _, exists := values[k]; exists {
				return nil, "", errors.New("duplicate ConfigMap data key")
			}
			values[k] = append([]byte(nil), v...)
		}
		return values, cm.ResourceVersion, nil
	}
	s.list = func(ctx context.Context, o metav1.ListOptions) (string, error) {
		r, err := client.List(ctx, o)
		if err != nil {
			return "", err
		}
		if r == nil {
			return "", errors.New("nil ConfigMap list")
		}
		return r.ResourceVersion, nil
	}
	s.watch = client.Watch
	return s
}

// Secret exposes decoded Data entries as strings. Base64 decoding belongs to the SDK.
func Secret(client SecretClient, name string, opts ...Option) *Source {
	s := newSource("secret", name, opts)
	if client == nil {
		return s
	}
	s.get = func(ctx context.Context) (map[string]any, string, error) {
		secret, err := client.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		if secret == nil {
			return nil, "", errors.New("nil Secret")
		}
		values := map[string]any{}
		for k, v := range secret.Data {
			values[k] = string(v)
		}
		return values, secret.ResourceVersion, nil
	}
	s.list = func(ctx context.Context, o metav1.ListOptions) (string, error) {
		r, err := client.List(ctx, o)
		if err != nil {
			return "", err
		}
		if r == nil {
			return "", errors.New("nil Secret list")
		}
		return r.ResourceVersion, nil
	}
	s.watch = client.Watch
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) valid() bool {
	return s.get != nil && s.list != nil && s.watch != nil && s.name != "" && s.timeout > 0 && s.interval >= 0 && s.limit > 0 && ((s.key == "" && s.decode == nil) || (s.key != "" && s.decode != nil && s.bindings == nil))
}
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if !s.valid() {
		return x.Layer{}, errors.New("invalid Kubernetes source settings")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	values, revision, err := s.get(ctx)
	if apierrors.IsNotFound(err) {
		return x.Layer{}, x.ErrNotFound
	}
	if err != nil {
		return x.Layer{}, err
	}
	if err = ctx.Err(); err != nil {
		return x.Layer{}, err
	}
	if revision == "" {
		return x.Layer{}, errors.New("missing Kubernetes resourceVersion")
	}
	remaining := s.limit
	for _, value := range values {
		switch v := value.(type) {
		case string:
			remaining -= int64(len(v))
		case []byte:
			remaining -= int64(len(v))
		}
		if remaining < 0 {
			return x.Layer{}, errors.New("Kubernetes data exceeds size limit")
		}
	}
	if s.decode != nil {
		value, exists := values[s.key]
		if !exists {
			return x.Layer{}, x.ErrNotFound
		}
		var data []byte
		switch v := value.(type) {
		case string:
			data = []byte(v)
		case []byte:
			data = v
		default:
			return x.Layer{}, errors.New("invalid document data")
		}
		return document.Decode(data, s.decode, s.id+"/"+s.key, revision)
	}
	bindings := s.bindings
	if bindings == nil {
		bindings = map[string]x.Path{}
		for name := range values {
			bindings[name] = x.Path{name}
		}
	}
	paths := map[string]bool{}
	layer := x.Layer{Values: map[string]any{}, Locations: map[string]x.Location{}, Revision: revision}
	for name, path := range bindings {
		if len(path) == 0 {
			return x.Layer{}, errors.New("empty Kubernetes binding path")
		}
		key := path.String()
		for p := range paths {
			if p == key || strings.HasPrefix(p, key+"/") || strings.HasPrefix(key, p+"/") {
				return x.Layer{}, errors.New("overlapping Kubernetes bindings")
			}
		}
		paths[key] = true
		value, exists := values[name]
		if !exists {
			continue
		}
		dst := layer.Values
		for _, part := range path[:len(path)-1] {
			if dst[part] == nil {
				dst[part] = map[string]any{}
			}
			dst = dst[part].(map[string]any)
		}
		dst[path[len(path)-1]] = value
		layer.Locations[key] = x.Location{Name: s.id + "/" + name}
	}
	return layer, nil
}
func (s *Source) stream(ctx context.Context) (watch.Interface, context.CancelFunc, error) {
	listCtx, cancelList := context.WithTimeout(ctx, s.timeout)
	options := metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("metadata.name", s.name).String()}
	revision, err := s.list(listCtx, options)
	cancelList()
	if err != nil {
		return nil, nil, err
	}
	if revision == "" {
		return nil, nil, errors.New("missing list resourceVersion")
	}
	child, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(s.timeout, cancel)
	seconds := int64(300)
	options.ResourceVersion = revision
	options.AllowWatchBookmarks = true
	options.TimeoutSeconds = &seconds
	w, err := s.watch(child, options)
	timer.Stop()
	if err == nil {
		err = child.Err()
	}
	if err != nil || w == nil {
		if w != nil {
			w.Stop()
		}
		cancel()
		if err == nil {
			err = errors.New("nil Kubernetes watch")
		}
		return nil, nil, err
	}
	return w, cancel, nil
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if !s.valid() {
		return nil, errors.New("invalid Kubernetes source settings")
	}
	ctx, cancel := context.WithCancel(ctx)
	w, stopStream, err := s.stream(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if stopStream != nil {
				stopStream()
			}
			if w != nil {
				w.Stop()
			}
		}()
		backoff := 250 * time.Millisecond
		var ticks <-chan time.Time
		if s.interval > 0 {
			t := time.NewTicker(s.interval)
			defer t.Stop()
			ticks = t.C
		}
		for {
			if w == nil {
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				w, stopStream, err = s.stream(ctx)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					notify()
					backoff = min(backoff*2, 5*time.Second)
					continue
				}
				backoff = 250 * time.Millisecond
				notify()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticks:
				notify()
			case event, ok := <-w.ResultChan():
				if !ok || event.Type == watch.Error {
					stopStream()
					stopStream = nil
					w.Stop()
					w = nil
					notify()
					continue
				}
				switch event.Type {
				case watch.Added, watch.Modified, watch.Deleted:
					object, err := meta.Accessor(event.Object)
					if err != nil {
						stopStream()
						stopStream = nil
						w.Stop()
						w = nil
						notify()
						continue
					}
					if object.GetName() == s.name {
						notify()
					}
				case watch.Bookmark: // Progress only; a full read occurs after reconnection.
				}
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}
