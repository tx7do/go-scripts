package http

import (
	"net/http"
	"strings"
	"time"
)

// Option configures a [Reader]. Pass to [New].
type Option func(*configOptions)

// configOptions is the accumulator for Option values.
type configOptions struct {
	baseURL    string
	prefix     string
	timeout    time.Duration
	headers    map[string]string
	httpClient *http.Client      // for tests; when non-nil the default client is overridden
	transport  http.RoundTripper // for tests; injected into the client
}

// WithBaseURL sets the base URL that keys are resolved against. The key is
// appended to the base URL to form the full request URL.
//
// Example: WithBaseURL("https://api.example.com/scripts/") + key "main.lua"
//
//	-> GET https://api.example.com/scripts/main.lua
func WithBaseURL(baseURL string) Option {
	return func(o *configOptions) {
		// Ensure trailing slash so key appends cleanly.
		if baseURL != "" && !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		o.baseURL = baseURL
	}
}

// WithPrefix sets a key prefix that is transparently prepended to every key
// before it is resolved against the base URL. Useful when all scripts share a
// common path segment.
//
// Leading slashes are stripped; no trailing slash normalization is applied
// (the base URL should handle that).
func WithPrefix(prefix string) Option {
	return func(o *configOptions) {
		prefix = strings.TrimPrefix(prefix, "/")
		o.prefix = prefix
	}
}

// WithTimeout sets the HTTP client timeout (default 30s).
func WithTimeout(timeout time.Duration) Option {
	return func(o *configOptions) { o.timeout = timeout }
}

// WithHeader adds or overrides an HTTP header sent with every request.
// Multiple calls accumulate headers.
func WithHeader(key, value string) Option {
	return func(o *configOptions) {
		if o.headers == nil {
			o.headers = make(map[string]string)
		}
		o.headers[key] = value
	}
}

// WithHTTPClient injects a custom *http.Client. When set, WithTimeout is
// ignored (the caller controls the client's timeout).
func WithHTTPClient(c *http.Client) Option {
	return func(o *configOptions) { o.httpClient = c }
}
