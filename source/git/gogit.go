package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	sshauth "github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

// goGit implements gitAPI using the go-git library.
type goGit struct {
	auth authConfig
}

// newGoGit creates a goGit instance with the given auth configuration.
func newGoGit(cfg *configOptions) *goGit {
	return &goGit{auth: cfg.auth}
}

// Clone implements gitAPI.Clone.
func (g *goGit) Clone(ctx contextWrap, repoURL, localPath, branch string, depth int) error {
	ref := plumbing.ReferenceName(branch)
	if branch == "HEAD" || branch == "" {
		ref = plumbing.HEAD
	} else if !isFullRef(branch) {
		// Try as branch first, then tag.
		ref = plumbing.NewBranchReferenceName(branch)
	}

	opts := &gogit.CloneOptions{
		URL:      repoURL,
		Progress: nil,
	}
	if ref.String() != "" && ref.String() != "HEAD" {
		opts.ReferenceName = ref
	}
	if depth > 0 {
		opts.Depth = depth
	}

	// Set auth if configured.
	if clientOpts := g.buildClientOptions(); len(clientOpts) > 0 {
		opts.ClientOptions = clientOpts
	}

	_, err := gogit.PlainClone(localPath, opts)
	if err != nil {
		// If branch-as-branch failed, try as tag.
		if branch != "HEAD" && branch != "" && !isFullRef(branch) {
			opts.ReferenceName = plumbing.NewTagReferenceName(branch)
			_, err2 := gogit.PlainClone(localPath, opts)
			if err2 == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

// Pull implements gitAPI.Pull.
func (g *goGit) Pull(ctx contextWrap, localPath string) error {
	repo, err := gogit.PlainOpen(localPath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	opts := &gogit.PullOptions{
		Progress: nil,
	}
	if clientOpts := g.buildClientOptions(); len(clientOpts) > 0 {
		opts.ClientOptions = clientOpts
	}

	if err := wt.Pull(opts); err != nil {
		if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			return nil
		}
		return err
	}
	return nil
}

// ReadFile implements gitAPI.ReadFile.
func (g *goGit) ReadFile(localPath, key string) ([]byte, error) {
	fullPath := filepath.Join(localPath, filepath.FromSlash(key))
	return os.ReadFile(fullPath)
}

// HeadHash implements gitAPI.HeadHash.
func (g *goGit) HeadHash(localPath string) (string, error) {
	repo, err := gogit.PlainOpen(localPath)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}

	return head.Hash().String(), nil
}

// Cleanup implements gitAPI.Cleanup.
func (g *goGit) Cleanup(localPath string) error {
	return os.RemoveAll(localPath)
}

// buildClientOptions constructs the appropriate client.Option slice based on
// the auth configuration. Returns nil if no auth is configured.
func (g *goGit) buildClientOptions() []client.Option {
	// Token auth (highest priority for HTTPS).
	if g.auth.token != "" {
		return []client.Option{
			client.WithHTTPAuth(&http.BasicAuth{
				Username: "token", // GitHub / GitLab expect any username with token as password.
				Password: g.auth.token,
			}),
		}
	}

	// Username/password auth.
	if g.auth.username != "" && g.auth.password != "" {
		return []client.Option{
			client.WithHTTPAuth(&http.BasicAuth{
				Username: g.auth.username,
				Password: g.auth.password,
			}),
		}
	}

	// SSH key auth.
	if g.auth.sshKey != "" {
		publicKeys, err := sshauth.NewPublicKeysFromFile("git", g.auth.sshKey, "")
		if err != nil {
			return nil
		}
		return []client.Option{
			client.WithSSHAuth(publicKeys),
		}
	}

	return nil
}

// isFullRef checks if the branch string is already a full reference name
// (e.g., "refs/heads/main" or "refs/tags/v1.0").
func isFullRef(branch string) bool {
	return len(branch) > 5 && (branch[:5] == "refs/" || branch == "HEAD")
}

// Ensure goGit implements gitAPI at compile time.
var _ gitAPI = (*goGit)(nil)

// Unused but kept for potential future use of fs.FS abstraction.
var _ fs.FS = (*fsSource)(nil)

type fsSource struct{ dir string }

func (f *fsSource) Open(name string) (fs.File, error) {
	return os.Open(filepath.Join(f.dir, name))
}
