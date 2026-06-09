# Redis Source · Redis スクリプトソース

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Redis KV ベースのスクリプトソース、値比較ホットリロード検出対応**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Redis Source は [go-redis/v9](https://github.com/redis/go-redis) を使用して Redis または互換サーバー（Valkey、DragonflyDB、KeyDB など）からスクリプトを読み取ります。クライアント側での値比較ポーリングによりホットリロードを検出します。

### 特徴

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/redis/go-redis/v9` |
| ホットリロード | クライアント側値比較ポーリング |
| キー接頭辞 | `WithPrefix` で名前空間を分離 |
| 認証 | パスワード / ACL ユーザー名+パスワード対応 |
| 互換性 | Redis、Valkey、DragonflyDB、KeyDB |
| インターフェース | `source.ReadWatcher` を実装 |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/source/redis
```

---

## 設定オプション

| オプション | デフォルト | 説明 |
| --- | --- | --- |
| `WithAddr(addr)` | `localhost:6379` | Redis サーバーアドレス |
| `WithPassword(pass)` | 空 | 認証パスワード |
| `WithUsername(user)` | 空 | ACL ユーザー名（Redis 6.0+） |
| `WithDB(db)` | `0` | 論理データベースインデックス |
| `WithPrefix(prefix)` | 空 | キー接頭辞 |

---

## クイックスタート

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

## テスト

```bash
cd source/redis && go test -v ./...
```

---

## 関連ドキュメント

- [Source ドキュメントに戻る](../README_JP.md)
- [メインドキュメントに戻る](../../README_JP.md)

## ライセンス

[MIT License](../../LICENSE)
