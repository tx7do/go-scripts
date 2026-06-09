# etcd Source · etcd 脚本来源

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**基于 etcd KV 存储的脚本来源，支持原生 Watch 热更新**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

etcd Source 使用 [etcd clientv3](https://github.com/etcd-io/etcd) 从 etcd 集群读取脚本。利用 etcd 原生的 Watch API 实现高效的热更新检测，无需轮询。

### 特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `go.etcd.io/etcd/client/v3` |
| 热更新 | etcd 原生 Watch（PUT / DELETE 事件触发） |
| Key 前缀 | 支持 `WithPrefix` 命名空间隔离 |
| 认证 | 支持用户名/密码 |
| 接口 | 实现 `source.ReadWatcher` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/source/etcd
```

---

## 配置选项

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `WithEndpoints(addr...)` | `localhost:2379` | etcd 服务器地址 |
| `WithUsername(user)` | 空 | 认证用户名 |
| `WithPassword(pass)` | 空 | 认证密码 |
| `WithPrefix(prefix)` | 空 | Key 前缀（自动去除前导 `/`） |
| `WithTimeout(d)` | 5s | 连接超时 |

---

## 快速开始

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

    // 加载脚本
    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)

    // 热更新监听
    ch, _ := src.Watch(ctx, "hello.lua")
    go func() {
        for range ch {
            fmt.Println("脚本已变更，请重新加载")
        }
    }()
}
```

---

## 测试

```bash
cd source/etcd && go test -v ./...
```

---

## 相关文档

- [返回 Source 文档](../README.md)
- [返回主文档](../../README.md)

## License

[MIT License](../../LICENSE)
