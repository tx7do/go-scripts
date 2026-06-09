package http

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"time"
)

// Watch returns a channel that signals when the content at the URL identified
// by `key` changes. It polls the URL every 5 seconds, computes the CRC32
// checksum of the response body, and sends a signal when the checksum differs
// from the one recorded during the last Load.
//
// The returned channel is closed when the context is cancelled. Callers
// should re-Load the script after receiving from the channel.
func (r *Reader) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	// Verify the key has been loaded at least once.
	r.mu.RLock()
	_, ok := r.checksums[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("http source: key %q has not been loaded yet; call Load first", key)
	}

	ch := make(chan struct{})

	go func() {
		defer close(ch)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if r.hasContentChanged(ctx, key) {
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	return ch, nil
}

// hasContentChanged fetches the URL and compares the CRC32 checksum against
// the stored value. Returns true if the content differs.
func (r *Reader) hasContentChanged(ctx context.Context, key string) bool {
	url := r.resolveKey(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	currentSum := crc32.ChecksumIEEE(body)

	r.mu.RLock()
	stored := r.checksums[key]
	r.mu.RUnlock()

	return currentSum != stored
}
