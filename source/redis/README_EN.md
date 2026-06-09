# Redis Source · Redis Script Source

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Redis KV-backed script source with value comparison hot-reload detection**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Redis Source uses [go-redis/v9](https://github.com/redis/go-redis) to read scripts from Redis or compatible servers (Valkey, DragonflyDB, KeyDB, etc.). Hot-reload is detected by client-side value comparison polling.

### Features

| Feature | Description |
| --- | --- |
| Library | `github.com/redis/go-redis/v9` |
| Hot-Reload | Client-side value comparison polling |
| Key Prefix | `WithPrefix` for namespace isolation |
| Auth | Password / ACL username+password |
| Compatibility | Redis, Valkey, DragonflyDB, KeyDB |
| Interface | Implements `source.ReadWatcher` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/source/redis
```

---

## Configuration Options

| Option | Default | Description |
| --- | --- | --- |
| `WithAddr(addr)` | `localhost:6379` | Redis server address |
| `WithPassword(pass)` | empty | Auth password |
| `WithUsername(user)` | empty | ACL username (Redis 6.0+) |
| `WithDB(db)` | `0` | Logical database index |
| `WithPrefix(prefix)` | empty | Key prefix |

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    redisSrc "github.com/tx7do/go-scripts/source/redis"
)

func main() {
    ctx := context.Background()

    src, err := redisSrc.New(ctx,
        redisSrc.WithAddr("localhost:6379"),
        redisSrc.WithPrefix("scripts:lua:"),
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
cd source/redis && go test -v ./...
```

---

## Related Documentation

- [Back to Source Documentation](../README_EN.md)
- [Back to Main Documentation](../../README_EN.md)

## License

[MIT License](../../LICENSE)
