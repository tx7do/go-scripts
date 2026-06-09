# HTTP Source · HTTP Script Source

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**HTTP(S)-based remote script source with CRC32 checksum hot-reload detection**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The HTTP Source fetches scripts via HTTP(S) from web servers, CDN endpoints, REST APIs, or any HTTP-accessible resource. Hot-reload is detected by comparing the response body's CRC32 checksum.

### Features

| Feature | Description |
| --- | --- |
| Library | `net/http` (standard library) |
| Hot-Reload | CRC32 checksum comparison polling |
| Custom Headers | `WithHeader` for authentication, etc. |
| Base URL | `WithBaseURL` + key forms the full URL |
| Key Prefix | `WithPrefix` for path prefix |
| Interface | Implements `source.ReadWatcher` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/source/http
```

---

## Configuration Options

| Option | Default | Description |
| --- | --- | --- |
| `WithBaseURL(url)` | **required** | Base URL; key is appended to it |
| `WithPrefix(prefix)` | empty | Key prefix (leading `/` stripped) |
| `WithTimeout(d)` | 30s | HTTP client timeout |
| `WithHeader(k, v)` | empty | Custom header (can be called multiple times) |
| `WithHTTPClient(c)` | default Client | Inject a custom `*http.Client` |

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "time"
    httpSrc "github.com/tx7do/go-scripts/source/http"
)

func main() {
    ctx := context.Background()

    src, err := httpSrc.New(ctx,
        httpSrc.WithBaseURL("https://api.example.com/scripts/"),
        httpSrc.WithHeader("Authorization", "Bearer token123"),
        httpSrc.WithTimeout(10*time.Second),
    )
    if err != nil { panic(err) }
    defer src.Close()

    // Load script (URL: https://api.example.com/scripts/hello.lua)
    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

---

## Testing

```bash
cd source/http && go test -v ./...
```

---

## Related Documentation

- [Back to Source Documentation](../README_EN.md)
- [Back to Main Documentation](../../README_EN.md)

## License

[MIT License](../../LICENSE)
