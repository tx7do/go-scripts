# HTTP Source · HTTP 脚本来源

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**基于 HTTP(S) 协议的远程脚本来源，支持 CRC32 校验和热更新检测**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

HTTP Source 通过 HTTP(S) 协议从 Web 服务器、CDN、REST API 或任何 HTTP 可访问资源获取脚本。通过比对响应体的 CRC32 校验和检测热更新。

### 特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `net/http`（标准库） |
| 热更新 | CRC32 校验和比对轮询 |
| 自定义请求头 | 支持 `WithHeader` 设置认证等 |
| Base URL | `WithBaseURL` + key 拼接完整 URL |
| Key 前缀 | 支持 `WithPrefix` 路径前缀 |
| 接口 | 实现 `source.ReadWatcher` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/source/http
```

---

## 配置选项

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `WithBaseURL(url)` | **必填** | 基础 URL，key 追加到其后 |
| `WithPrefix(prefix)` | 空 | Key 前缀（自动去除前导 `/`） |
| `WithTimeout(d)` | 30s | HTTP 客户端超时 |
| `WithHeader(k, v)` | 空 | 自定义请求头（可多次调用） |
| `WithHTTPClient(c)` | 默认 Client | 注入自定义 `*http.Client` |

---

## 快速开始

```go
package main

import (
    "context"
    "fmt"
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

    // 加载脚本（URL: https://api.example.com/scripts/hello.lua）
    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

---

## 测试

```bash
cd source/http && go test -v ./...
```

---

## 相关文档

- [返回 Source 文档](../README.md)
- [返回主文档](../../README.md)

## License

[MIT License](../../LICENSE)
