# Consul Source · Consul 脚本来源

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**基于 HashiCorp Consul KV 存储的脚本来源，支持 ModifyIndex 轮询热更新**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Consul Source 使用 [Consul API](https://github.com/hashicorp/consul) 从 Consul KV 存储读取脚本。通过轮询 Key 的 `ModifyIndex` 变化来检测热更新。

### 特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/hashicorp/consul/api` |
| 热更新 | ModifyIndex 轮询（每 5 秒） |
| Key 前缀 | 支持 `WithPrefix` 命名空间隔离 |
| 认证 | 支持 ACL Token |
| 接口 | 实现 `source.ReadWatcher` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/source/consul
```

---

## 配置选项

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `WithAddress(addr)` | `127.0.0.1:8500` | Consul Agent 地址 |
| `WithToken(token)` | 空 | ACL 认证 Token |
| `WithPrefix(prefix)` | 空 | Key 前缀（自动去除前导 `/`） |
| `WithTimeout(d)` | 5s | HTTP 超时 |

---

## 快速开始

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

## 测试

```bash
cd source/consul && go test -v ./...
```

---

## 相关文档

- [返回 Source 文档](../README.md)
- [返回主文档](../../README.md)

## License

[MIT License](../../LICENSE)
