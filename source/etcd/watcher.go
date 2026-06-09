package etcd

import (
	"context"
)

// Watch returns a channel that signals when the value identified by `key`
// changes. It uses etcd's native Watch API for efficient event-driven
// notification without polling.
//
// The returned channel is closed when the context is cancelled. Callers
// should re-Load the script after receiving from the channel.
func (r *Reader) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	// Verify the key has been loaded at least once.
	r.mu.RLock()
	_, ok := r.values[key]
	r.mu.RUnlock()
	if !ok {
		return nil, errNotLoaded(key)
	}

	etcdKey := r.resolveKey(key)

	// Start etcd watch on the key.
	watchCh := r.client.Watch(ctx, etcdKey)

	// Buffered so a signal is never lost if the receiver hasn't started
	// draining yet.
	ch := make(chan struct{}, 1)

	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case resp, ok := <-watchCh:
				if !ok {
					// Watch channel closed (context cancelled or server disconnect).
					return
				}
				if resp.Canceled {
					return
				}
				// Signal on any PUT or DELETE event.
				if len(resp.Events) > 0 {
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
	return "etcd source: key \"" + e.key + "\" has not been loaded yet; call Load first"
}
