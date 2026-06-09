# Git Source · Git Script Source

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Git repository-backed script source with commit hash comparison hot-reload detection**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Git Source uses [go-git](https://github.com/go-git/go-git) to read scripts from a remote Git repository or a local Git repository. On initialization, it automatically clones the repository to a local temporary directory. Hot-reload is detected by polling for new commits via hash comparison.

### Features

| Feature | Description |
| --- | --- |
| Library | `github.com/go-git/go-git/v5` (pure Go, zero CGO) |
| Hot-Reload | Commit hash comparison polling (`git pull`) |
| Auth | Token / username+password / SSH Key |
| Shallow Clone | `WithDepth` to limit clone depth |
| Local Repo | `WithLocalPath` to open a local repo directly |
| Key Prefix | `WithPrefix` for namespace isolation |
| Interface | Implements `source.ReadWatcher` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/source/git
```

---

## Configuration Options

| Option | Default | Description |
| --- | --- | --- |
| `WithRepoURL(url)` | **required*** | Remote Git repository URL |
| `WithBranch(branch)` | `HEAD` | Branch / tag / ref name |
| `WithPrefix(prefix)` | empty | Key prefix (leading `/` stripped) |
| `WithLocalPath(path)` | empty | Local repo path (alternative to RepoURL) |
| `WithAuth(user, pass)` | empty | Username + password auth |
| `WithToken(token)` | empty | Bearer token (GitHub PAT / GitLab Token) |
| `WithSSHKey(path)` | empty | SSH private key file path |
| `WithDepth(n)` | 0 (full clone) | Shallow clone depth |
| `WithPullInterval(d)` | 30s | Watch poll interval |

> \* When using `WithLocalPath`, `WithRepoURL` is not needed.

---

## Quick Start

### Load from Remote Repository

```go
package main

import (
    "context"
    "fmt"
    gitSrc "github.com/tx7do/go-scripts/source/git"
)

func main() {
    ctx := context.Background()

    src, err := gitSrc.New(ctx,
        gitSrc.WithRepoURL("https://github.com/user/scripts.git"),
        gitSrc.WithBranch("main"),
        gitSrc.WithPrefix("scripts/lua/"),
    )
    if err != nil { panic(err) }
    defer src.Close()

    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

### Token Authentication (GitHub)

```go
src, err := gitSrc.New(ctx,
    gitSrc.WithRepoURL("https://github.com/my-org/scripts.git"),
    gitSrc.WithToken("ghp_xxxxxxxxxxxx"),
    gitSrc.WithDepth(1),  // shallow clone
)
```

### Local Repository

```go
src, err := gitSrc.New(ctx,
    gitSrc.WithLocalPath("/path/to/local/repo"),
    gitSrc.WithPrefix("lua/"),
)
```

### Hot-Reload Watching

```go
// Load first to establish baseline
_, _ = src.Load(ctx, "main.lua")

// Start watching (periodic git pull + HEAD hash comparison)
ch, _ := src.Watch(ctx, "main.lua")

for range ch {
    // Remote repo has new commits
    code, _ := src.Load(ctx, "main.lua")
    fmt.Println("reloaded:", code)
}
```

---

## Hot-Reload Mechanism

```
Every 30s (default):
  git pull
    ↓
  Get current HEAD commit hash
    ↓
  Compare with baseline hash recorded at Load time
    ↓
  Different -> send change signal
```

- **Independent Watcher Tracking**: Each `Watch()` call maintains its own baseline; multiple watchers don't interfere.
- **Pull Failure Tolerance**: When Pull fails (network issues, etc.), the current tick is skipped and retried next interval.
- **No Change = No Signal**: No signal is sent when the HEAD hash hasn't changed.

---

## Error Handling

| Error | Description |
| --- | --- |
| `ErrNotFound` | File does not exist; identify with `errors.Is(err, gitSrc.ErrNotFound)` or `gitSrc.IsNotFound(err)` |
| Others | Wraps original error with prefix `git source: ...` |

---

## Testing

```bash
cd source/git && go test -v ./...
```

Test coverage (25 cases, all passing, no real Git server required):

| Category | Cases |
| --- | --- |
| Interface implementation | Compile-time assertion |
| Construction validation | `WithRepoURL` or `WithLocalPath` required |
| Prefix normalization | 6 prefix variations table-driven test |
| Load | Happy path / not found (ErrNotFound) / with prefix / context cancelled |
| Watch | Change detected / no change / context cancelled / not loaded error / pull failure tolerance / concurrent watchers |
| Concurrency | 30 goroutine concurrent Load |
| Options | WithRepoURL / WithBranch / WithAuth / WithToken / WithDepth / WithPullInterval |
| Local repo | Load from local directory |

---

## Related Documentation

- [Back to Source Documentation](../README_EN.md)
- [Back to Main Documentation](../../README_EN.md)

## License

[MIT License](../../LICENSE)
