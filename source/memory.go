package source

import (
	"context"
	"fmt"
	"sync"
)

type memEntry struct {
	code string
}

// MemSource keeps scripts in memory.
// Suitable for dynamic short-lived scripts, unit tests, or RPC-pushed snippets;
// zero IO overhead.
type MemSource struct {
	mu       sync.Mutex
	data     map[string]string
	watchers map[string][]chan struct{} // key -> list of watch channels
}

// NewMemSource creates a MemSource.
func NewMemSource() *MemSource {
	return &MemSource{
		data:     make(map[string]string),
		watchers: make(map[string][]chan struct{}),
	}
}

// Set inserts or overwrites a script.
func (ms *MemSource) Set(key, code string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.data[key] = code
	// Notify all watchers for this key.
	if chs, ok := ms.watchers[key]; ok {
		for _, ch := range chs {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// Delete removes a script.
func (ms *MemSource) Delete(key string) {
	ms.mu.Lock()
	delete(ms.data, key)
	// Notify all watchers for this key.
	if chs, ok := ms.watchers[key]; ok {
		for _, ch := range chs {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	ms.mu.Unlock()
}

// Load returns the script for the given key.
func (ms *MemSource) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	code, ok := ms.data[key]
	if !ok {
		return "", fmt.Errorf("mem source: key %q not found", key)
	}
	return code, nil
}

// Close is a no-op: MemSource holds no resources that need releasing.
func (ms *MemSource) Close() error { return nil }

// Compile-time assertion: *MemSource implements source.ReadWatcher.
var _ ReadWatcher = (*MemSource)(nil)

// Watch returns a channel that signals when the script identified by `key` changes.
// The channel receives a signal whenever Set or Delete is called for that key.
//
// The returned channel is closed when the context is cancelled. Callers should
// re-Load the script after receiving from the channel.
func (ms *MemSource) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	ch := make(chan struct{}, 1) // buffered to avoid missing notifications

	ms.mu.Lock()
	ms.watchers[key] = append(ms.watchers[key], ch)
	ms.mu.Unlock()

	go func() {
		<-ctx.Done()
		// Remove the channel from watchers to prevent memory leaks.
		ms.mu.Lock()
		defer ms.mu.Unlock()
		if chs, ok := ms.watchers[key]; ok {
			for i, c := range chs {
				if c == ch {
					ms.watchers[key] = append(chs[:i], chs[i+1:]...)
					break
				}
			}
		}
		close(ch)
	}()

	return ch, nil
}
