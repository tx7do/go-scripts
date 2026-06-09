# Redis Source · Redis 脚本来源

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**基于 Redis KV 存储的脚本来源，支持值比对热更新检测**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Redis Source 使用 [go-redis/v9](https://github.com/redis/go-redis) 从 Redis 或兼容服务器（Valkey、DragonflyDB、KeyDB 等）读取脚本。通过值比对轮询检测热更新。

### 特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/redis/go-redis/v9` |
| 热更新 | 客户端值比对轮询 |
| Key 前缀 | 支持 `WithPrefix` 命名空间隔离 |
| 认证 | 支持密码 / ACL 用户名+密码 |
| 兼容性 | Redis、Valkey、DragonflyDB、KeyDB |
| 接口 | 实现 `source.ReadWatcher` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/source/redis
```

---

## 配置选项

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `WithAddr(addr)` | `localhost:6379` | Redis 服务器地址 |
| `WithPassword(pass)` | 空 | 认证密码 |
| `WithUsername(user)` | 空 | ACL 用户名（Redis 6.0+） |
| `WithDB(db)` | `0` | 逻辑数据库索引 |
| `WithPrefix(prefix)` | 空 | Key 前缀 |

---

## 快速开始

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

## 测试

```bash
cd source/redis && go test -v ./...
```

---

## 相关文档

- [返回 Source 文档](../README.md)
- [返回主文档](../../README.md)

## License

[MIT License](../../LICENSE)
