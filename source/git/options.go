package git

import (
	"strings"
	"time"
)

// Option configures a [Reader]. Pass to [New].
type Option func(*configOptions)

// configOptions is the accumulator for Option values.
type configOptions struct {
	repoURL      string // remote URL to clone from
	branch       string // branch / tag / ref (default "HEAD")
	prefix       string // key prefix (e.g. "scripts/lua/")
	localPath    string // if set, use this local repo instead of cloning
	auth         authConfig
	depth        int           // shallow clone depth (0 = full clone)
	pullInterval time.Duration // poll interval for Watch (default 30s)
	cleanup      bool          // whether to remove the cloned dir on Close

	// For test injection
	gitOp gitAPI
}

// authConfig holds authentication settings for the remote git server.
type authConfig struct {
	username string
	password string
	sshKey   string // path to SSH private key
	token    string // bearer token (for GitHub / GitLab etc.)
}

// ioCloser is the minimal close interface.
type ioCloser interface {
	Close() error
}

// gitAPI is the subset of git operations this package depends on. Defining
// it as an interface lets tests substitute a fake without a real git server.
type gitAPI interface {
	// Clone clones the remote repo into the given local path.
	Clone(ctx contextWrap, repoURL, localPath, branch string, depth int) error

	// Pull fetches and merges the latest changes from the remote.
	Pull(ctx contextWrap, localPath string) error

	// ReadFile reads a file from the working tree at the given relative path.
	ReadFile(localPath, key string) ([]byte, error)

	// HeadHash returns the current HEAD commit hash.
	HeadHash(localPath string) (string, error)

	// Cleanup removes the local clone directory.
	Cleanup(localPath string) error
}

// contextWrap is a minimal interface to avoid importing context in this file.
type contextWrap interface {
	Done() <-chan struct{}
	Err() error
}

// WithRepoURL sets the remote git repository URL to clone from.
//
//	WithRepoURL("https://github.com/user/scripts.git")
//	WithRepoURL("git@github.com:user/scripts.git")
func WithRepoURL(url string) Option {
	return func(o *configOptions) { o.repoURL = url }
}

// WithBranch sets the branch, tag, or ref to checkout (default "HEAD").
func WithBranch(branch string) Option {
	return func(o *configOptions) { o.branch = branch }
}

// WithPrefix sets a path prefix that is transparently prepended to every key.
// Leading slashes are stripped.
//
//	WithPrefix("scripts/lua/") + key "main.lua" -> "scripts/lua/main.lua"
func WithPrefix(prefix string) Option {
	return func(o *configOptions) {
		o.prefix = strings.TrimPrefix(prefix, "/")
	}
}

// WithLocalPath sets a local repository path. When set, the Reader will open
// the local repo directly instead of cloning from a remote URL. This is useful
// for development / testing.
func WithLocalPath(path string) Option {
	return func(o *configOptions) { o.localPath = path }
}

// WithAuth sets the authentication credentials for the remote git server.
func WithAuth(username, password string) Option {
	return func(o *configOptions) {
		o.auth.username = username
		o.auth.password = password
	}
}

// WithToken sets a bearer token for authentication (GitHub PAT, GitLab token, etc.).
func WithToken(token string) Option {
	return func(o *configOptions) { o.auth.token = token }
}

// WithSSHKey sets the path to an SSH private key for authentication.
func WithSSHKey(keyPath string) Option {
	return func(o *configOptions) { o.auth.sshKey = keyPath }
}

// WithDepth sets the shallow clone depth (0 = full clone).
func WithDepth(depth int) Option {
	return func(o *configOptions) { o.depth = depth }
}

// WithPullInterval sets the polling interval for Watch (default 30s).
func WithPullInterval(d time.Duration) Option {
	return func(o *configOptions) { o.pullInterval = d }
}
