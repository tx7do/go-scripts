package s3

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// Option configures a [Source]. Pass to [New].
type Option func(*configOptions)

// configOptions is the accumulator for Option values.
type configOptions struct {
	region    string
	endpoint  string
	pathStyle bool
	prefix    string
	creds     aws.CredentialsProvider
	anonymous bool
	client    s3API // for tests; when non-nil the AWS SDK is bypassed
}

// WithRegion sets the AWS region (e.g. "us-east-1"). When omitted the default
// AWS SDK chain resolves the region from env / shared config.
func WithRegion(region string) Option {
	return func(o *configOptions) { o.region = region }
}

// WithEndpoint overrides the AWS endpoint URL. Use this to point at MinIO,
// Alibaba OSS, Tencent COS, Cloudflare R2, LocalStack, etc.
func WithEndpoint(endpoint string) Option {
	return func(o *configOptions) { o.endpoint = endpoint }
}

// WithPathStyle forces path-style addressing
// (https://endpoint/bucket/key instead of https://bucket.endpoint/key).
// Required by MinIO and some other self-hosted S3-compatible servers.
func WithPathStyle() Option {
	return func(o *configOptions) { o.pathStyle = true }
}

// WithPrefix sets a key prefix that is transparently prepended to every key
// before it is resolved against the bucket. Useful when all scripts share a
// common directory inside the bucket (e.g. WithPrefix("scripts/lua/")).
//
// Leading slashes are stripped and a trailing slash is added automatically,
// so "scripts", "/scripts", "scripts/" all normalize to "scripts/".
func WithPrefix(prefix string) Option {
	return func(o *configOptions) {
		prefix = strings.TrimPrefix(prefix, "/")
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		o.prefix = prefix
	}
}

// WithCredentials supplies an explicit credentials provider. By default the
// standard AWS credential chain (env vars / shared config / IMDS / ECS) is
// used.
func WithCredentials(p aws.CredentialsProvider) Option {
	return func(o *configOptions) { o.creds = p }
}

// WithStaticCredentials is a shortcut for WithCredentials with a static
// access-key / secret-key pair. Pass an empty token unless the credentials
// are temporary (STS).
func WithStaticCredentials(accessKey, secretKey, token string) Option {
	return WithCredentials(credentials.NewStaticCredentialsProvider(accessKey, secretKey, token))
}

// WithAnonymous disables authentication entirely. Use for public buckets or
// for endpoints (such as test fakes) that don't validate signatures.
func WithAnonymous() Option {
	return func(o *configOptions) { o.anonymous = true }
}
