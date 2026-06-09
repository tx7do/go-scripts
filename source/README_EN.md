# Source · Script Source Module

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Core interfaces for script loading and change watching in Go Scripts**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Source module defines the core interfaces for script sources, providing unified script loading and hot-reload capabilities for all engines. It contains three core interfaces and five built-in implementations.

### Core Interfaces

| Interface | Description |
| --- | --- |
| `Reader` | Script loading interface, `Load(ctx, key)` loads script source by key |
| `Watcher` | Change watching interface, `Watch(ctx, key)` returns a change notification channel |
| `ReadWatcher` | `Reader` + `Watcher` combined interface |

### Built-in Implementations

| Implementation | File | Hot-Reload Mechanism | Description |
| --- | --- | --- | --- |
| `FileSource` | `file.go` | mtime polling | Local filesystem |
| `FileSystemSource` | `fs.go` | Not supported (immutable sources) | Based on `io/fs.FS` (embed / zip / os.DirFS) |
| `MemSource` | `memory.go` | Embedded channel notification | In-memory storage, for testing and injection |
| `MultiSource` | `multiple.go` | Forwards child Source events | Multi-source aggregation (Fallback / FirstOK) |

### Extension Sub-modules

| Module | Library | Hot-Reload Mechanism |
| --- | --- | --- |
| [s3](s3/README_EN.md) | AWS SDK v2 | ETag comparison polling |
| [etcd](etcd/README_EN.md) | etcd clientv3 | Native Watch API |
| [consul](consul/README_EN.md) | Consul API | ModifyIndex polling |
| [redis](redis/README_EN.md) | go-redis/v9 | Value comparison polling |
| [http](http/README_EN.md) | net/http | CRC32 checksum comparison |
| [git](git/README_EN.md) | go-git/v6 | commit hash comparison polling |

---

## Interface Definition

```go
// Reader - script loading
type Reader interface {
    Load(ctx context.Context, key string) (code string, err error)
    Close() error
}

// Watcher - change watching
type Watcher interface {
    Watch(ctx context.Context, key string) (<-chan struct{}, error)
}

// ReadWatcher - combined interface
type ReadWatcher interface {
    Reader
    Watcher
}
```

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "os"
    "github.com/tx7do/go-scripts/source"
)

func main() {
    ctx := context.Background()

    // Using FileSource
    fileSrc := source.NewFileSource()
    defer fileSrc.Close()
    code, _ := fileSrc.Load(ctx, "/path/to/script.lua")
    fmt.Println(code)

    // Using FileSystemSource (based on io/fs.FS)
    // Works with go:embed, archive/zip, os.DirFS, etc.
    fsSrc, _ := source.NewFileSystemSource(os.DirFS("/scripts"))
    defer fsSrc.Close()
    code, _ = fsSrc.Load(ctx, "hello.lua")

    // Using MemSource
    memSrc := source.NewMemSource()
    defer memSrc.Close()
    memSrc.Set("hello.lua", `print("hello")`)
    code, _ = memSrc.Load(ctx, "hello.lua")

    // Fallback aggregation (File → Memory fallback)
    multi, _ := source.NewFallbackSource(fileSrc, memSrc)
    code, _ = multi.Load(ctx, "backup.lua")
}
```

---

## MultiSource Aggregation Strategies

| Strategy | Constructor | Description |
| --- | --- | --- |
| **Fallback** | `NewFallbackSource(srcs...)` | Tries each Source in order, returns first success |
| **FirstOK** | `NewFirstOKSource(srcs...)` | Requests all Sources concurrently, returns first success |

---

## Testing

```bash
cd source && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [S3 Source](s3/README_EN.md)
- [etcd Source](etcd/README_EN.md)
- [Consul Source](consul/README_EN.md)
- [Redis Source](redis/README_EN.md)
- [HTTP Source](http/README_EN.md)
- [Git Source](git/README_EN.md)

## License

[MIT License](../LICENSE)
