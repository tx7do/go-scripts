package source

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
)

// FSSourceOption configures a FileSystemSource.
type FSSourceOption func(*FileSystemSource)

// WithFSPrefix sets a prefix prepended to every key before lookup in the
// underlying fs.FS. Leading slashes are stripped from the prefix; a trailing
// slash is appended if missing.
//
// Example: WithFSPrefix("scripts") + key "main.lua" -> "scripts/main.lua"
func WithFSPrefix(prefix string) FSSourceOption {
	return func(s *FileSystemSource) {
		p := strings.TrimPrefix(prefix, "/")
		if p != "" && !strings.HasSuffix(p, "/") {
			p += "/"
		}
		s.prefix = p
	}
}

// FileSystemSource reads scripts from any io/fs.FS implementation.
//
// This enables several powerful patterns:
//   - go:embed: bake scripts into the binary at compile time (zero external
//     file dependencies at runtime).
//   - archive/zip: read scripts directly from .zip / .pak archives without
//     writing any decompression logic.
//   - os.DirFS: read from a real directory through the fs.FS abstraction.
//   - Custom fs.FS implementations (in-memory, remote, etc.).
//
// FileSystemSource implements Reader. It does NOT implement Watcher because
// most fs.FS implementations (embed.FS, zip.Reader) are immutable. For
// mutable filesystems with hot-reload support, use FileSource instead.
type FileSystemSource struct {
	fsys   fs.FS
	prefix string
}

// NewFileSystemSource creates a FileSystemSource from the given fs.FS.
// Returns an error if fsys is nil.
//
// Example with go:embed:
//
//	//go:embed scripts/*.lua
//	var embedFS embed.FS
//
//	src, err := NewFileSystemSource(embedFS, WithFSPrefix("scripts"))
func NewFileSystemSource(fsys fs.FS, opts ...FSSourceOption) (*FileSystemSource, error) {
	if fsys == nil {
		return nil, fmt.Errorf("fs source: underlying fs.FS is nil")
	}
	s := &FileSystemSource{fsys: fsys}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Load reads the file at the given key from the underlying fs.FS.
// The key is resolved as: prefix + key (with leading slashes stripped).
func (s *FileSystemSource) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// fs.FS requires keys without leading slashes.
	key = strings.TrimPrefix(key, "/")
	fullKey := s.prefix + key

	b, err := fs.ReadFile(s.fsys, fullKey)
	if err != nil {
		return "", fmt.Errorf("fs source: read %q: %w", fullKey, err)
	}
	return string(b), nil
}

// Close is a no-op: the underlying fs.FS is owned by the caller and should
// be closed (if needed) by its creator.
func (s *FileSystemSource) Close() error { return nil }

// Compile-time assertion: *FileSystemSource implements source.Reader.
var _ Reader = (*FileSystemSource)(nil)
