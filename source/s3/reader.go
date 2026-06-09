// Package s3 provides a [source.Reader] implementation that reads
// scripts from an Amazon S3 bucket (or any S3-compatible object storage such
// as MinIO, Alibaba OSS, Tencent COS, Cloudflare R2, LocalStack, ...).
//
// Construction:
//
//	src, err := s3.New(ctx, "my-bucket",
//	    s3.WithRegion("us-east-1"),
//	    s3.WithPrefix("scripts/lua/"),
//	)
//
// Hot-reload detection compares the object's ETag and LastModified against the
// values recorded by the most recent Load; a subsequent ReloadCheck reports
// true when either changes.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/tx7do/go-scripts/source"
)

// Reader reads scripts from an S3 bucket.
//
// All exported methods are safe for concurrent use. Reader implements the
// [source.Reader] interface.
type Reader struct {
	client s3API

	bucket string
	prefix string

	mu       sync.RWMutex
	versions map[string]version // by user-facing key (before prefix is applied)
}

// version captures the fields used to detect object mutation.
type version struct {
	etag     string
	modified time.Time
}

// s3API is the subset of *s3.Client this package depends on. Defining it as
// an interface lets tests substitute a fake without spinning up a real S3
// endpoint, and ensures we don't accidentally grow our surface area on the
// SDK's concrete type.
type s3API interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// Compile-time assertion: *Reader implements source.Reader.
var _ source.Reader = (*Reader)(nil)

// Compile-time assertion: *Reader also implements source.ReadWatcher.
var _ source.ReadWatcher = (*Reader)(nil)

// withClient is an internal option used by tests to inject a fake [s3API].
// Not exported.
func withClient(c s3API) Option {
	return func(o *configOptions) { o.client = c }
}

// New creates an S3-backed [Reader]. `bucket` is required; all other settings
// are optional.
//
// Credentials are loaded via the default AWS SDK v2 chain (env vars / shared
// config / IMDS / ...). Override with WithCredentials / WithStaticCredentials
// / WithAnonymous.
//
// For S3-compatible services pass WithEndpoint("https://...") and (typically)
// WithPathStyle().
func New(ctx context.Context, bucket string, opts ...Option) (*Reader, error) {
	if bucket == "" {
		return nil, errors.New("s3 source: bucket must be non-empty")
	}

	cfg := &configOptions{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Test-injected client bypasses the AWS SDK entirely.
	if cfg.client != nil {
		return &Reader{
			client:   cfg.client,
			bucket:   bucket,
			prefix:   cfg.prefix,
			versions: make(map[string]version),
		}, nil
	}

	var loadOpts []func(*config.LoadOptions) error
	if cfg.region != "" {
		loadOpts = append(loadOpts, config.WithRegion(cfg.region))
	}
	if cfg.anonymous {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(aws.AnonymousCredentials{}))
	} else if cfg.creds != nil {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(cfg.creds))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3 source: load aws config: %w", err)
	}

	var clientOpts []func(*s3.Options)
	if cfg.endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.endpoint)
		})
	}
	if cfg.pathStyle {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, clientOpts...)

	return &Reader{
		client:   client,
		bucket:   bucket,
		prefix:   cfg.prefix,
		versions: make(map[string]version),
	}, nil
}

// resolveKey prepends the configured prefix to the user-supplied key.
func (r *Reader) resolveKey(key string) string {
	return r.prefix + key
}

// Load fetches the object from S3 and returns its body as a string.
// Context cancellation propagates to the underlying request.
//
// A "404 / NoSuchKey" response is reported as a wrapped [ErrNotFound].
// Other errors are wrapped with the object key for easier debugging.
func (r *Reader) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	objKey := r.resolveKey(key)
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(objKey),
	})
	if err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, objKey)
		}
		return "", fmt.Errorf("s3 source: get object %q: %w", objKey, err)
	}
	defer out.Body.Close()

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return "", fmt.Errorf("s3 source: read body %q: %w", objKey, err)
	}

	r.mu.Lock()
	r.versions[key] = version{
		etag:     aws.ToString(out.ETag),
		modified: aws.ToTime(out.LastModified),
	}
	r.mu.Unlock()

	return string(body), nil
}

// Close releases any underlying resources. The AWS SDK v2 S3 client has no
// explicit Close method, so this is currently a no-op; the method exists so
// Reader satisfies [source.Reader] and so future implementations can
// release resources (custom HTTP transports, pools, ...) here.
func (r *Reader) Close() error { return nil }
