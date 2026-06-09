package source

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// FileSource reads scripts from the local filesystem.
// It has no extra dependencies and is the default choice for dev/debug.
type FileSource struct {
	mu     sync.RWMutex
	mtimes map[string]time.Time // key -> file mtime recorded at the last Load
}

// NewFileSource creates a FileSource.
func NewFileSource() *FileSource {
	return &FileSource{mtimes: make(map[string]time.Time)}
}

// Load reads the local file at the given key (path).
func (fs *FileSource) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(key)
	if err != nil {
		return "", fmt.Errorf("file source: read %q: %w", key, err)
	}
	if fi, statErr := os.Stat(key); statErr == nil {
		fs.mu.Lock()
		fs.mtimes[key] = fi.ModTime()
		fs.mu.Unlock()
	}
	return string(b), nil
}

// Close is a no-op: FileSource holds no resources that need releasing.
func (fs *FileSource) Close() error { return nil }

// Compile-time assertion: *FileSource implements source.ReadWatcher.
var _ ReadWatcher = (*FileSource)(nil)

// Watch returns a channel that signals when the file identified by `key` changes.
// It polls the file's modification time every second and sends a signal on the
// channel when a change is detected.
//
// The returned channel is closed when the context is cancelled. Callers should
// re-Load the script after receiving from the channel.
func (fs *FileSource) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	ch := make(chan struct{})

	// Get initial mtime.
	fi, err := os.Stat(key)
	if err != nil {
		return nil, fmt.Errorf("file source: stat %q: %w", key, err)
	}
	lastMod := fi.ModTime()

	go func() {
		defer close(ch)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fi, err := os.Stat(key)
				if err != nil {
					// File deleted or inaccessible; skip this tick.
					continue
				}
				if fi.ModTime().After(lastMod) {
					lastMod = fi.ModTime()
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
