# etcd Source · etcd Script Source

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**etcd KV-backed script source with native Watch hot-reload**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The etcd Source uses [etcd clientv3](https://github.com/etcd-io/etcd) to read scripts from an etcd cluster. It leverages etcd's native Watch API for efficient hot-reload detection without polling.

### Features

| Feature | Description |
| --- | --- |
| Library | `go.etcd.io/etcd/client/v3` |
| Hot-Reload | etcd native Watch (PUT / DELETE events) |
| Key Prefix | `WithPrefix` for namespace isolation |
| Auth | Username/password support |
| Interface | Implements `source.ReadWatcher` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/source/etcd
```

---

## Configuration Options

| Option | Default | Description |
| --- | --- | --- |
| `WithEndpoints(addr...)` | `localhost:2379` | etcd server endpoints |
| `WithUsername(user)` | empty | Auth username |
| `WithPassword(pass)` | empty | Auth password |
| `WithPrefix(prefix)` | empty | Key prefix (leading `/` stripped) |
| `WithTimeout(d)` | 5s | Connection timeout |

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    etcdSrc "github.com/tx7do/go-scripts/source/etcd"
)

func main() {
    ctx := context.Background()

    src, err := etcdSrc.New(ctx,
        etcdSrc.WithEndpoints("localhost:2379"),
        etcdSrc.WithPrefix("scripts/lua/"),
    )
    if err != nil { panic(err) }
    defer src.Close()

    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

---

## Testing

```bash
cd source/etcd && go test -v ./...
```

---

## Related Documentation

- [Back to Source Documentation](../README_EN.md)
- [Back to Main Documentation](../../README_EN.md)

## License

[MIT License](../../LICENSE)
