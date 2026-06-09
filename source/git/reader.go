// Package git provides a [source.Reader] implementation that reads
// scripts from a git repository. It clones the repo to a local temp directory
// and reads files from the working tree. Hot-reload is supported by polling
// for new commits (git pull + HEAD hash comparison).
//
// Construction:
//
//	src, err := git.New(ctx,
//	    git.WithRepoURL("https://github.com/user/scripts.git"),
//	    git.WithBranch("main"),
//	    git.WithPrefix("scripts/lua/"),
//	)
//
// Hot-reload uses commit-hash polling: the Watcher periodically runs git pull
// and compares the HEAD hash with the one recorded at the last Load.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tx7do/go-scripts/source"
)

// Reader reads scripts from a git repository.
//
// All exported methods are safe for concurrent use. Reader implements the
// [source.ReadWatcher] interface.
type Reader struct {
	op     gitAPI // git operations (real or fake)
	path   string // local path to the cloned/opened repo
	prefix string

	pullInterval time.Duration

	mu      sync.RWMutex
	hashes  map[string]string // key -> last HEAD hash at Load time
	cleanup bool              // whether we own the temp dir
	closed  bool
}

// Compile-time assertion: *Reader implements source.Reader.
var _ source.Reader = (*Reader)(nil)

// Compile-time assertion: *Reader also implements source.ReadWatcher.
var _ source.ReadWatcher = (*Reader)(nil)

// defaultPullInterval is the default polling interval for Watch.
const defaultPullInterval = 30 * time.Second

// withGitOp is an internal option used by tests to inject a fake [gitAPI].
// Not exported.
func withGitOp(op gitAPI) Option {
	return func(o *configOptions) {
		o.gitOp = op
	}
}

// New creates a git-backed [Reader].
//
// At minimum, either WithRepoURL or WithLocalPath must be supplied.
// When WithRepoURL is used, the repo is cloned to a temporary directory.
// When WithLocalPath is used, the repo is opened directly from the local
// filesystem (no clone).
func New(ctx context.Context, opts ...Option) (*Reader, error) {
	cfg := &configOptions{
		branch:       "HEAD",
		pullInterval: defaultPullInterval,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Test-injected gitAPI bypasses real git operations entirely.
	if cfg.gitOp != nil {
		r := &Reader{
			op:           cfg.gitOp,
			path:         cfg.localPath,
			prefix:       cfg.prefix,
			pullInterval: cfg.pullInterval,
			hashes:       make(map[string]string),
			cleanup:      false,
		}
		return r, nil
	}

	// Validate configuration.
	if cfg.repoURL == "" && cfg.localPath == "" {
		return nil, errors.New("git source: either WithRepoURL or WithLocalPath is required")
	}

	var localPath string
	var needsCleanup bool

	if cfg.localPath != "" {
		// Use the provided local path directly.
		localPath = cfg.localPath
		needsCleanup = false
	} else {
		// Clone to a temp directory.
		tmpDir, err := os.MkdirTemp("", "go-scripts-git-*")
		if err != nil {
			return nil, fmt.Errorf("git source: create temp dir: %w", err)
		}
		localPath = tmpDir
		needsCleanup = true

		// Create the real git operator and clone.
		op := newGoGit(cfg)
		if err := op.Clone(ctx, cfg.repoURL, localPath, cfg.branch, cfg.depth); err != nil {
			_ = os.RemoveAll(localPath)
			return nil, fmt.Errorf("git source: clone %q: %w", cfg.repoURL, err)
		}
	}

	// Determine which gitAPI to use.
	var op gitAPI
	if cfg.gitOp != nil {
		op = cfg.gitOp
	} else {
		op = newGoGit(cfg)
	}

	return &Reader{
		op:           op,
		path:         localPath,
		prefix:       cfg.prefix,
		pullInterval: cfg.pullInterval,
		hashes:       make(map[string]string),
		cleanup:      needsCleanup,
	}, nil
}

// resolveKey prepends the configured prefix to the user-supplied key.
func (r *Reader) resolveKey(key string) string {
	return r.prefix + key
}

// Load reads the file at the given key from the git working tree.
// Context cancellation propagates to the underlying read.
//
// An absent file is reported as a wrapped [ErrNotFound].
func (r *Reader) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	resolved := r.resolveKey(key)

	b, err := r.op.ReadFile(r.path, resolved)
	if err != nil {
		if os.IsNotExist(err) || isNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, resolved)
		}
		return "", fmt.Errorf("git source: read %q: %w", resolved, err)
	}

	// Record the current HEAD hash for this key (used by Watch).
	if hash, hashErr := r.op.HeadHash(r.path); hashErr == nil {
		r.mu.Lock()
		r.hashes[key] = hash
		r.mu.Unlock()
	}

	return string(b), nil
}

// Close releases resources. If the Reader cloned the repo to a temp directory,
// the directory is removed.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	if r.cleanup && r.path != "" {
		return r.op.Cleanup(r.path)
	}
	return nil
}

// IsNotFound reports whether err represents a "file not found" error.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// isNotExist checks various "file not found" error patterns.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no such file") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "not found")
}

// joinPath is a helper that joins path components using forward slashes
// (git repos use forward-slash paths regardless of OS).
func joinPath(parts ...string) string {
	return filepath.ToSlash(filepath.Join(parts...))
}
