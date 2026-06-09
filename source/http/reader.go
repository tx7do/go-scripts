// Package http provides a [source.Reader] implementation that fetches
// scripts via HTTP(S). It is suitable for retrieving scripts from web
// servers, CDN endpoints, REST APIs, or any HTTP-accessible resource.
//
// Construction:
//
//	src, err := http.New(ctx,
//	    http.WithBaseURL("https://api.example.com/scripts/"),
//	    http.WithTimeout(10*time.Second),
//	    http.WithHeader("Authorization", "Bearer token"),
//	)
//
// Hot-reload detection compares the response body's checksum (CRC32) against
// the value recorded by the most recent Load.
package http

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tx7do/go-scripts/source"
)

// Reader fetches scripts via HTTP.
//
// All exported methods are safe for concurrent use. Reader implements the
// [source.ReadWatcher] interface.
type Reader struct {
	client  *http.Client
	headers map[string]string

	baseURL string
	prefix  string

	mu        sync.RWMutex
	checksums map[string]uint32 // key -> CRC32 of last loaded body
}

// Compile-time assertion: *Reader implements source.Reader.
var _ source.Reader = (*Reader)(nil)

// Compile-time assertion: *Reader also implements source.ReadWatcher.
var _ source.ReadWatcher = (*Reader)(nil)

// New creates an HTTP-backed [Reader]. WithBaseURL is required; all other
// settings are optional.
func New(_ context.Context, opts ...Option) (*Reader, error) {
	cfg := &configOptions{
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.baseURL == "" {
		return nil, fmt.Errorf("http source: base URL must be set via WithBaseURL")
	}

	client := cfg.httpClient
	if client == nil {
		client = &http.Client{Timeout: cfg.timeout}
	}

	return &Reader{
		client:    client,
		headers:   cfg.headers,
		baseURL:   cfg.baseURL,
		prefix:    cfg.prefix,
		checksums: make(map[string]uint32),
	}, nil
}

// resolveKey builds the full URL from baseURL + prefix + key.
func (r *Reader) resolveKey(key string) string {
	return r.baseURL + r.prefix + key
}

// Load fetches the URL via HTTP GET and returns the response body as a string.
// Context cancellation propagates to the underlying request.
//
// A 404 response is reported as a wrapped [ErrNotFound].
// Non-2xx responses (other than 404) are reported as errors.
func (r *Reader) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	url := r.resolveKey(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("http source: create request for %q: %w", url, err)
	}

	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http source: get %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%w: %s", ErrNotFound, url)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http source: get %q: HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("http source: read body %q: %w", url, err)
	}

	r.mu.Lock()
	r.checksums[key] = crc32.ChecksumIEEE(body)
	r.mu.Unlock()

	return string(body), nil
}

// Close releases the underlying HTTP client. The default http.Client has no
// resources to release, so this is a no-op; the method exists to satisfy the
// [source.Reader] interface.
func (r *Reader) Close() error {
	r.client.CloseIdleConnections()
	return nil
}

// IsNotFound reports whether err represents a 404 response. Equivalent to
// errors.Is(err, ErrNotFound).
func IsNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrNotFound.Error())
}
