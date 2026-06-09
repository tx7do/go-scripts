package git

import (
	"context"
	"fmt"
	"time"
)

// Watch returns a channel that signals when the git repository receives new
// commits that may affect the file identified by `key`.
//
// It works by periodically running `git pull` and comparing the HEAD commit
// hash with the one recorded at the last Load. If the hash has changed, a
// signal is sent on the channel.
//
// The returned channel is closed when the context is cancelled. Callers
// should re-Load the script after receiving from the channel.
func (r *Reader) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	// Verify the key has been loaded at least once.
	r.mu.RLock()
	baseline, ok := r.hashes[key]
	r.mu.RUnlock()
	if !ok {
		return nil, errNotLoaded(key)
	}

	interval := r.pullInterval
	if interval <= 0 {
		interval = defaultPullInterval
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
				// Attempt to pull latest changes.
				if err := r.op.Pull(ctx, r.path); err != nil {
					// Pull failed (network issue, merge conflict, etc.);
					// skip this tick and try again next interval.
					continue
				}

				// Compare HEAD hash with our own baseline.
				currentHash, err := r.op.HeadHash(r.path)
				if err != nil {
					continue
				}

				if currentHash != baseline {
					baseline = currentHash

					// Non-blocking send.
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
	return fmt.Sprintf("git source: key %q has not been loaded yet; call Load first", e.key)
}
