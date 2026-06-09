# Consul Source · Consul Script Source

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**HashiCorp Consul KV-backed script source with ModifyIndex polling hot-reload**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Consul Source uses the [Consul API](https://github.com/hashicorp/consul) to read scripts from a Consul KV store. Hot-reload is detected by polling the key's `ModifyIndex` for changes.

### Features

| Feature | Description |
| --- | --- |
| Library | `github.com/hashicorp/consul/api` |
| Hot-Reload | ModifyIndex polling (every 5 seconds) |
| Key Prefix | `WithPrefix` for namespace isolation |
| Auth | ACL Token support |
| Interface | Implements `source.ReadWatcher` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/source/consul
```

---

## Configuration Options

| Option | Default | Description |
| --- | --- | --- |
| `WithAddress(addr)` | `127.0.0.1:8500` | Consul Agent address |
| `WithToken(token)` | empty | ACL auth token |
| `WithPrefix(prefix)` | empty | Key prefix (leading `/` stripped) |
| `WithTimeout(d)` | 5s | HTTP timeout |

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    consulSrc "github.com/tx7do/go-scripts/source/consul"
)

func main() {
    ctx := context.Background()

    src, err := consulSrc.New(ctx,
        consulSrc.WithAddress("127.0.0.1:8500"),
        consulSrc.WithPrefix("scripts/lua/"),
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
cd source/consul && go test -v ./...
```

---

## Related Documentation

- [Back to Source Documentation](../README_EN.md)
- [Back to Main Documentation](../../README_EN.md)

## License

[MIT License](../../LICENSE)
