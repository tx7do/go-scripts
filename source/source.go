package source

import (
	"context"
)

// Reader represents a script source.
// key is the unique identifier of a script: path / object key / script id, etc.,
// interpreted by the concrete implementation.
type Reader interface {
	// Load loads the script source code.
	Load(ctx context.Context, key string) (code string, err error)

	// Close releases underlying resources (s3 client, file handles, etc.).
	Close() error
}

// Watcher defines an optional capability to observe changes for a given key.
// Implementations that support hot-reload can return a channel that signals
// when the underlying source has been modified.
type Watcher interface {
	// Watch returns a channel that receives a signal whenever the script
	// identified by `key` changes. The caller should re-Load the script
	// after receiving from the channel.
	//
	// The returned error indicates whether watching could be established
	// (e.g., unsupported key, permission denied). Once Watch succeeds,
	// the channel is closed when the context is cancelled or the watcher
	// encounters an unrecoverable error.
	Watch(ctx context.Context, key string) (<-chan struct{}, error)
}

// ReadWatcher combines Reader and Watcher into a single interface.
// Use this when you need both script loading and change notification
// capabilities from the same source.
type ReadWatcher interface {
	Reader
	Watcher
}
