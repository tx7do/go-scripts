# Source · 脚本来源模块

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Go Scripts 的脚本加载与变更监听核心接口**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Source 模块定义了脚本来源的核心接口，为所有引擎提供统一的脚本加载和热更新能力。它包含三个核心接口和五种内置实现。

### 核心接口

| 接口 | 说明 |
| --- | --- |
| `Reader` | 脚本加载接口，`Load(ctx, key)` 按键加载脚本源码 |
| `Watcher` | 变更监听接口，`Watch(ctx, key)` 返回变更通知 channel |
| `ReadWatcher` | `Reader` + `Watcher` 组合接口 |

### 内置实现

| 实现 | 文件 | 热更新机制 | 说明 |
| --- | --- | --- | --- |
| `FileSource` | `file.go` | mtime 轮询 | 本地文件系统 |
| `FileSystemSource` | `fs.go` | 不支持（不可变源） | 基于 `io/fs.FS`（embed / zip / os.DirFS） |
| `MemSource` | `memory.go` | 内嵌 channel 通知 | 内存存储，用于测试和注入 |
| `MultiSource` | `multiple.go` | 转发子 Source 事件 | 多源聚合（Fallback / FirstOK） |

### 扩展子模块

| 子模块 | 底层库 | 热更新机制 |
| --- | --- | --- |
| [s3](s3/README.md) | AWS SDK v2 | ETag 比对轮询 |
| [etcd](etcd/README.md) | etcd clientv3 | 原生 Watch API |
| [consul](consul/README.md) | Consul API | ModifyIndex 轮询 |
| [redis](redis/README.md) | go-redis/v9 | 值比对轮询 |
| [http](http/README.md) | net/http | CRC32 校验和比对 |
| [git](git/README.md) | go-git/v6 | commit hash 比对轮询 |

---

## 接口定义

```go
// Reader - 脚本加载
type Reader interface {
    Load(ctx context.Context, key string) (code string, err error)
    Close() error
}

// Watcher - 变更监听
type Watcher interface {
    Watch(ctx context.Context, key string) (<-chan struct{}, error)
}

// ReadWatcher - 组合接口
type ReadWatcher interface {
    Reader
    Watcher
}
```

---

## 快速开始

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

    // 使用 FileSource
    fileSrc := source.NewFileSource()
    defer fileSrc.Close()
    code, _ := fileSrc.Load(ctx, "/path/to/script.lua")
    fmt.Println(code)

    // 使用 FileSystemSource（基于 io/fs.FS）
    // 适用于 go:embed、archive/zip、os.DirFS 等
    fsSrc, _ := source.NewFileSystemSource(os.DirFS("/scripts"))
    defer fsSrc.Close()
    code, _ = fsSrc.Load(ctx, "hello.lua")

    // 使用 MemSource
    memSrc := source.NewMemSource()
    defer memSrc.Close()
    memSrc.Set("hello.lua", `print("hello")`)
    code, _ = memSrc.Load(ctx, "hello.lua")

    // Fallback 聚合（File → Memory 回退）
    multi, _ := source.NewFallbackSource(fileSrc, memSrc)
    code, _ = multi.Load(ctx, "backup.lua")
}
```

---

## MultiSource 聚合策略

| 策略 | 构造函数 | 说明 |
| --- | --- | --- |
| **Fallback** | `NewFallbackSource(srcs...)` | 按顺序尝试每个 Source，第一个成功即返回 |
| **FirstOK** | `NewFirstOKSource(srcs...)` | 并发请求所有 Source，返回最先成功的结果 |

---

## 测试

```bash
cd source && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [S3 Source 文档](s3/README.md)
- [etcd Source 文档](etcd/README.md)
- [Consul Source 文档](consul/README.md)
- [Redis Source 文档](redis/README.md)
- [HTTP Source 文档](http/README.md)
- [Git Source 文档](git/README.md)

## License

[MIT License](../LICENSE)
