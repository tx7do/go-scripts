# Source · 脚本来源模块

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Go Scripts 的脚本加载与变更监听核心接口**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Source 模块定义了脚本来源的核心接口，为所有引擎提供统一的脚本加载和热更新能力。它包含三个核心接口、六种内置实现和多个扩展子模块。

### 核心接口

| 接口 | 说明 |
| --- | --- |
| `Reader` | 脚本加载接口，`Load(ctx, key)` 按键加载脚本源码 |
| `Watcher` | 变更监听接口，`Watch(ctx, key)` 返回变更通知 channel |
| `ReadWatcher` | `Reader` + `Watcher` 组合接口 |
| `TransformFunc` | 源码变换钩子，用于解密/解压/过滤（中间件模式） |

### 内置实现

| 实现 | 文件 | 热更新机制 | 说明 |
| --- | --- | --- | --- |
| `FileSource` | `file.go` | mtime 轮询 | 本地文件系统 |
| `FileSystemSource` | `fs.go` | 不支持（不可变源） | 基于 `io/fs.FS`（embed / zip / os.DirFS） |
| `MemSource` | `memory.go` | 内嵌 channel 通知 | 内存存储，用于测试和注入 |
| `MultiSource` | `multiple.go` | 转发子 Source 事件 | 多源聚合（Fallback / FirstOK） |
| `TransformSource` | `transform.go` | 透传内部源 | **中间件**：解密/解压/过滤钩子链 |
| `CachedSource` | `cached.go` | 自动失效缓存 | **缓存层**：内存缓存 + TTL + Watch 自动失效 |

### 扩展子模块

| 子模块 | 底层库 | 热更新机制 |
| --- | --- | --- |
| [s3](s3/README.md) | AWS SDK v2 | ETag 比对轮询 |
| [etcd](etcd/README.md) | etcd clientv3 | 原生 Watch API |
| [consul](consul/README.md) | Consul API | ModifyIndex 轮询 |
| [redis](redis/README.md) | go-redis/v9 | 值比对轮询 |
| [http](http/README.md) | net/http | CRC32 校验和比对 |
| [git](git/README.md) | go-git/v6 | commit hash 比对轮询 |
| [database](database/README.md) | database/sql | checksum 列比对轮询 |

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

    // CachedSource：S3 远程源 + 内存缓存
    // cached, _ := source.NewCachedSource(s3Source, source.WithTTL(5*time.Minute))
    // defer cached.Close()
    // code, _ = cached.Load(ctx, "script.lua")

    // TransformSource：解密中间件
    // decrypt := source.TransformFunc(func(key, raw string) (string, error) {
    //     return aesDecrypt(raw, secretKey)
    // })
    // transformSrc, _ := source.NewTransformSource(cached, decrypt)
    // defer transformSrc.Close()
    // code, _ = transformSrc.Load(ctx, "script.lua")
}
```

---

## TransformSource 源码变换中间件

`TransformSource` 在 `Load` 之后、返回给引擎之前，对脚本源码执行可配置的变换链。

典型场景：
- **解密**：S3 / Git / DB 中存储的 AES/XXTEA 加密脚本
- **解压**：gzip / zstd 压缩的脚本
- **过滤**：去除敏感信息、编码转换

```go
// 解密钩子
decrypt := source.TransformFunc(func(key string, raw string) (string, error) {
    return aesDecrypt(raw, secretKey)
})

// 包装 S3 Source
src, _ := source.NewTransformSource(s3Source, decrypt)

// 链式追加：解密后再解压
src, _ = src.Then(decompress)
```

---

## CachedSource 缓存层

`CachedSource` 在远程源（S3 / DB / HTTP）之上添加内存缓存，大幅减少网络 IO。

特性：
- 缓存命中时零网络开销
- 支持 TTL 自动过期（`WithTTL`）
- 如果远程源实现 `Watcher`，变更信号自动失效缓存
- 支持手动失效（`Invalidate` / `InvalidateAll`）

```go
// S3 + 内存缓存，TTL 5 分钟
cached, _ := source.NewCachedSource(s3Source, source.WithTTL(5*time.Minute))

// 叠加解密中间件
decrypted, _ := source.NewTransformSource(cached, decrypt)

code, _ := decrypted.Load(ctx, "script.lua")
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
- [Database Source 文档](database/README.md)

## License

[MIT License](../LICENSE)
