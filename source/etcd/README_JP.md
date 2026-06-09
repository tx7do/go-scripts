# etcd Source · etcd スクリプトソース

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**etcd KV ベースのスクリプトソース、ネイティブ Watch ホットリロード対応**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

etcd Source は [etcd clientv3](https://github.com/etcd-io/etcd) を使用して etcd クラスタからスクリプトを読み取ります。etcd のネイティブ Watch API を活用し、ポーリングなしで効率的なホットリロード検出を実現します。

### 特徴

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `go.etcd.io/etcd/client/v3` |
| ホットリロード | etcd ネイティブ Watch（PUT / DELETE イベント） |
| キー接頭辞 | `WithPrefix` で名前空間を分離 |
| 認証 | ユーザー名/パスワード対応 |
| インターフェース | `source.ReadWatcher` を実装 |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/source/etcd
```

---

## 設定オプション

| オプション | デフォルト | 説明 |
| --- | --- | --- |
| `WithEndpoints(addr...)` | `localhost:2379` | etcd サーバーアドレス |
| `WithUsername(user)` | 空 | 認証ユーザー名 |
| `WithPassword(pass)` | 空 | 認証パスワード |
| `WithPrefix(prefix)` | 空 | キー接頭辞（先頭の `/` は削除） |
| `WithTimeout(d)` | 5s | 接続タイムアウト |

---

## クイックスタート

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

    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

---

## テスト

```bash
cd source/etcd && go test -v ./...
```

---

## 関連ドキュメント

- [Source ドキュメントに戻る](../README_JP.md)
- [メインドキュメントに戻る](../../README_JP.md)

## ライセンス

[MIT License](../../LICENSE)
