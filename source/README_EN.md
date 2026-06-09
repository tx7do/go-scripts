# Source · Script Source Module

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Core interfaces for script loading and change watching in Go Scripts**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Source module defines the core interfaces for script sources, providing unified script loading and hot-reload capabilities for all engines. It contains three core interfaces, six built-in implementations, and multiple extension sub-modules.

### Core Interfaces

| Interface | Description |
| --- | --- |
| `Reader` | Script loading interface, `Load(ctx, key)` loads script source by key |
| `Watcher` | Change watching interface, `Watch(ctx, key)` returns a change notification channel |
| `ReadWatcher` | `Reader` + `Watcher` combined interface |
| `TransformFunc` | Source transform hook for decryption / decompression / filtering (middleware pattern) |

### Built-in Implementations

| Implementation | File | Hot-Reload Mechanism | Description |
| --- | --- | --- | --- |
| `FileSource` | `file.go` | mtime polling | Local filesystem |
| `FileSystemSource` | `fs.go` | Not supported (immutable sources) | Based on `io/fs.FS` (embed / zip / os.DirFS) |
| `MemSource` | `memory.go` | Embedded channel notification | In-memory storage, for testing and injection |
| `MultiSource` | `multiple.go` | Forwards child Source events | Multi-source aggregation (Fallback / FirstOK) |
| `TransformSource` | `transform.go` | Pass-through from inner source | **Middleware**: decryption / decompression / filtering hook chain |
| `CachedSource` | `cached.go` | Auto-invalidating cache | **Cache layer**: in-memory cache + TTL + Watch auto-invalidation |

### Extension Sub-modules

| Module | Library | Hot-Reload Mechanism |
| --- | --- | --- |
| [s3](s3/README_EN.md) | AWS SDK v2 | ETag comparison polling |
| [etcd](etcd/README_EN.md) | etcd clientv3 | Native Watch API |
| [consul](consul/README_EN.md) | Consul API | ModifyIndex polling |
| [redis](redis/README_EN.md) | go-redis/v9 | Value comparison polling |
| [http](http/README_EN.md) | net/http | CRC32 checksum comparison |
| [git](git/README_EN.md) | go-git/v6 | commit hash comparison polling |
| [database](database/README_EN.md) | database/sql | checksum column comparison polling |

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

    // CachedSource: S3 remote + in-memory cache
    // cached, _ := source.NewCachedSource(s3Source, source.WithTTL(5*time.Minute))
    // defer cached.Close()
    // code, _ = cached.Load(ctx, "script.lua")

    // TransformSource: decryption middleware
    // decrypt := source.TransformFunc(func(key, raw string) (string, error) {
    //     return aesDecrypt(raw, secretKey)
    // })
    // transformSrc, _ := source.NewTransformSource(cached, decrypt)
    // defer transformSrc.Close()
    // code, _ = transformSrc.Load(ctx, "script.lua")
}
```

---

## TransformSource Middleware

`TransformSource` applies a configurable transform chain to the loaded source code, after `Load` returns from the inner reader but before handing it to the engine.

Typical use cases:
- **Decryption**: AES/XXTEA encrypted scripts stored in S3 / Git / DB
- **Decompression**: gzip / zstd compressed scripts
- **Filtering**: strip sensitive info, encoding conversion

```go
// Decryption hook
decrypt := source.TransformFunc(func(key string, raw string) (string, error) {
    return aesDecrypt(raw, secretKey)
})

// Wrap S3 Source
src, _ := source.NewTransformSource(s3Source, decrypt)

// Chain: decrypt then decompress
src, _ = src.Then(decompress)
```

---

## CachedSource Cache Layer

`CachedSource` adds an in-memory cache on top of a remote source (S3 / DB / HTTP), dramatically reducing network IO.

Features:
- Zero network overhead on cache hit
- TTL-based auto-expiry (`WithTTL`)
- If the remote source implements `Watcher`, watch signals auto-invalidate the cache
- Manual invalidation (`Invalidate` / `InvalidateAll`)

```go
// S3 + in-memory cache, 5-minute TTL
cached, _ := source.NewCachedSource(s3Source, source.WithTTL(5*time.Minute))

// Layer decryption middleware on top
decrypted, _ := source.NewTransformSource(cached, decrypt)

code, _ := decrypted.Load(ctx, "script.lua")
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
- [Database Source](database/README_EN.md)

## License

[MIT License](../LICENSE)
