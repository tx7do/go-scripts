package redis

import (
	"context"
	"fmt"
	"time"
)

// Watch returns a channel that signals when the value identified by `key`
// changes. It polls the value every 5 seconds and sends a signal on the
// channel when the value differs from the one recorded during the last Load.
//
// The returned channel is closed when the context is cancelled. Callers should
// re-Load the script after receiving from the channel.
func (r *Reader) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	// Verify the key has been loaded at least once.
	r.mu.RLock()
	_, ok := r.values[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("redis source: key %q has not been loaded yet; call Load first", key)
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
				if r.hasValueChanged(ctx, key) {
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
