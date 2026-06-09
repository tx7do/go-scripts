package consul

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/consul/api"
)

// Watch returns a channel that signals when the value identified by `key`
// changes. It polls the key's ModifyIndex via Get every 5 seconds and sends a
// signal on the channel when the index differs from the one recorded during
// the last Load.
//
// The returned channel is closed when the context is cancelled. Callers
// should re-Load the script after receiving from the channel.
func (r *Reader) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	// Verify the key has been loaded at least once.
	r.mu.RLock()
	lastIdx, ok := r.indexs[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("consul source: key %q has not been loaded yet; call Load first", key)
	}

	consulKey := r.resolveKey(key)
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
				q := &api.QueryOptions{}
				q = q.WithContext(ctx)

				pair, _, err := r.client.Get(consulKey, q)
				if err != nil {
					// Network error; skip this tick.
					continue
				}
				if pair == nil {
					// Key was deleted; signal if not already.
					if lastIdx != 0 {
						lastIdx = 0
						select {
						case ch <- struct{}{}:
						default:
						}
					}
					continue
				}

				if pair.ModifyIndex != lastIdx {
					lastIdx = pair.ModifyIndex
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
