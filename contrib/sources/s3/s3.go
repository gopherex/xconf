// Package s3 reads documents from S3-compatible object storage using AWS SDK v2.
package s3

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/internal/document"
)

type Client interface {
	GetObject(context.Context, *sdk.GetObjectInput, ...func(*sdk.Options)) (*sdk.GetObjectOutput, error)
}
type Option func(*Source)
type Source struct {
	client                   Client
	bucket, key, id, version string
	decode                   x.Decoder
	limit                    int64
	interval, timeout        time.Duration
}

func Name(id string) Option           { return func(s *Source) { s.id = id } }
func Version(id string) Option        { return func(s *Source) { s.version = id } }
func MaxBytes(n int64) Option         { return func(s *Source) { s.limit = n } }
func Interval(d time.Duration) Option { return func(s *Source) { s.interval = d } }
func Timeout(d time.Duration) Option  { return func(s *Source) { s.timeout = d } }

// New accepts a configured client, including credentials, endpoint and path style.
// Interval(0) disables polling. Version pins a specific object version.
func New(client Client, bucket, key string, decode x.Decoder, opts ...Option) *Source {
	s := &Source{client: client, bucket: bucket, key: key, id: "s3://" + bucket + "/" + key, decode: decode, limit: document.DefaultMaxBytes, interval: 30 * time.Second, timeout: 10 * time.Second}
	for _, o := range opts {
		o(s)
	}
	return s
}
func (s *Source) Name() string { return s.id }
func (s *Source) Read(ctx context.Context, _ *sp.Schema) (x.Layer, error) {
	if s.client == nil || s.bucket == "" || s.key == "" || s.decode == nil || s.timeout <= 0 {
		return x.Layer{}, errors.New("invalid S3 source settings")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	input := &sdk.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key)}
	if s.version != "" {
		input.VersionId = aws.String(s.version)
	}
	out, err := s.client.GetObject(ctx, input)
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && (api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NoSuchVersion") {
			return x.Layer{}, x.ErrNotFound
		}
		return x.Layer{}, err
	}
	if out == nil || out.Body == nil {
		return x.Layer{}, errors.New("empty S3 response")
	}
	defer out.Body.Close()
	data, err := document.Read(ctx, out.Body, s.limit)
	if err != nil {
		return x.Layer{}, err
	}
	revision := aws.ToString(out.VersionId)
	if revision == "" || revision == "null" {
		revision = aws.ToString(out.ETag)
	}
	return document.Decode(data, s.decode, s.id, revision)
}
func (s *Source) Watch(ctx context.Context, notify func()) (func(), error) {
	if s.interval == 0 {
		return func() {}, nil
	}
	return x.Poll(s, s.interval).(x.Watcher).Watch(ctx, notify)
}
