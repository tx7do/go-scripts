package database

import (
	"context"
	"fmt"
	"time"
)

// Watch returns a channel that signals when the value identified by `key`
// changes. It polls the checksum column every pollInterval and sends a signal
// on the channel when the checksum differs from the one recorded during the
// last Load.
//
// The returned channel is closed when the context is cancelled. Callers should
// re-Load the script after receiving from the channel.
func (r *Reader) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	// Verify the key has been loaded at least once.
	r.mu.RLock()
	baseline, ok := r.checksums[key]
	r.mu.RUnlock()
	if !ok {
		return nil, errNotLoaded(key)
	}

	interval := r.pollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	ch := make(chan struct{}, 1)

	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Re-query the database for the current checksum.
				resolved := r.resolveKey(key)
				_, currentChecksum, err := r.api.GetScript(ctx, resolved)
				if err != nil {
					// Key was deleted or query failed; signal if it existed before.
					if IsNotFound(err) && baseline != "" {
						baseline = ""
						select {
						case ch <- struct{}{}:
						default:
						}
					}
					continue
				}

				if currentChecksum != baseline {
					baseline = currentChecksum

					// Non-blocking send to avoid goroutine leak if receiver is gone.
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

// errNotLoaded returns an error indicating the key must be loaded before watching.
func errNotLoaded(key string) error {
	return notLoadedError{key: key}
}

type notLoadedError struct{ key string }

func (e notLoadedError) Error() string {
	return fmt.Sprintf("database source: key %q has not been loaded yet; call Load first", e.key)
}
