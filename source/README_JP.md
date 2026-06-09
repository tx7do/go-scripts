# Source · スクリプトソースモジュール

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Go Scripts のスクリプト読み込みと変更監視コアインターフェース**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Source モジュールはスクリプトソースのコアインターフェースを定義し、すべてのエンジンに統一されたスクリプト読み込みとホットリロード機能を提供します。3 つのコアインターフェースと 5 つの組み込み実装を含みます。

### コアインターフェース

| インターフェース | 説明 |
| --- | --- |
| `Reader` | スクリプト読み込み、`Load(ctx, key)` でキーごとにソースを取得 |
| `Watcher` | 変更監視、`Watch(ctx, key)` で変更通知チャネルを返す |
| `ReadWatcher` | `Reader` + `Watcher` 統合インターフェース |

### 組み込み実装

| 実装 | ファイル | ホットリロード方式 | 説明 |
| --- | --- | --- | --- |
| `FileSource` | `file.go` | mtime ポーリング | ローカルファイルシステム |
| `FileSystemSource` | `fs.go` | 非対応（不変ソース） | `io/fs.FS` ベース（embed / zip / os.DirFS） |
| `MemSource` | `memory.go` | 組み込みチャネル通知 | メモリストレージ、テスト・注入用 |
| `MultiSource` | `multiple.go` | 子 Source のイベント転送 | マルチソース集約（Fallback / FirstOK） |

### 拡張サブモジュール

| モジュール | ライブラリ | ホットリロード方式 |
| --- | --- | --- |
| [s3](s3/README_JP.md) | AWS SDK v2 | ETag 比較ポーリング |
| [etcd](etcd/README_JP.md) | etcd clientv3 | ネイティブ Watch API |
| [consul](consul/README_JP.md) | Consul API | ModifyIndex ポーリング |
| [redis](redis/README_JP.md) | go-redis/v9 | 値比較ポーリング |
| [http](http/README_JP.md) | net/http | CRC32 チェックサム比較 |
| [git](git/README_JP.md) | go-git/v6 | commit hash 比較ポーリング |
| [database](database/README_JP.md) | database/sql | checksum 列比較ポーリング |

---

## インターフェース定義

```go
type Reader interface {
    Load(ctx context.Context, key string) (code string, err error)
    Close() error
}

type Watcher interface {
    Watch(ctx context.Context, key string) (<-chan struct{}, error)
}

type ReadWatcher interface {
    Reader
    Watcher
}
```

---

## クイックスタート

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

    fileSrc := source.NewFileSource()
    defer fileSrc.Close()
    code, _ := fileSrc.Load(ctx, "/path/to/script.lua")

    // FileSystemSource 使用（io/fs.FS ベース）
    // go:embed、archive/zip、os.DirFS などに対応
    fsSrc, _ := source.NewFileSystemSource(os.DirFS("/scripts"))
    defer fsSrc.Close()
    code, _ = fsSrc.Load(ctx, "hello.lua")

    memSrc := source.NewMemSource()
    defer memSrc.Close()
    memSrc.Set("hello.lua", `print("hello")`)
    code, _ = memSrc.Load(ctx, "hello.lua")

    multi, _ := source.NewFallbackSource(fileSrc, memSrc)
    code, _ = multi.Load(ctx, "backup.lua")
    fmt.Println(code)
}
```

---

## MultiSource 集約戦略

| 戦略 | コンストラクタ | 説明 |
| --- | --- | --- |
| **Fallback** | `NewFallbackSource(srcs...)` | 各 Source を順番に試し、最初の成功を返す |
| **FirstOK** | `NewFirstOKSource(srcs...)` | 全 Source に並行リクエスト、最初の成功を返す |

---

## テスト

```bash
cd source && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README_JP.md)
- [S3 Source](s3/README_JP.md)
- [etcd Source](etcd/README_JP.md)
- [Consul Source](consul/README_JP.md)
- [Redis Source](redis/README_JP.md)
- [HTTP Source](http/README_JP.md)
- [Git Source](git/README_JP.md)
- [Database Source](database/README_JP.md)

## ライセンス

[MIT License](../LICENSE)
