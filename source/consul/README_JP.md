# Consul Source · Consul スクリプトソース

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**HashiCorp Consul KV ベースのスクリプトソース、ModifyIndex ポーリングホットリロード対応**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Consul Source は [Consul API](https://github.com/hashicorp/consul) を使用して Consul KV ストアからスクリプトを読み取ります。キーの `ModifyIndex` 変更をポーリングしてホットリロードを検出します。

### 特徴

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/hashicorp/consul/api` |
| ホットリロード | ModifyIndex ポーリング（5 秒ごと） |
| キー接頭辞 | `WithPrefix` で名前空間を分離 |
| 認証 | ACL Token 対応 |
| インターフェース | `source.ReadWatcher` を実装 |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/source/consul
```

---

## 設定オプション

| オプション | デフォルト | 説明 |
| --- | --- | --- |
| `WithAddress(addr)` | `127.0.0.1:8500` | Consul Agent アドレス |
| `WithToken(token)` | 空 | ACL 認証 Token |
| `WithPrefix(prefix)` | 空 | キー接頭辞（先頭の `/` は削除） |
| `WithTimeout(d)` | 5s | HTTP タイムアウト |

---

## クイックスタート

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

## テスト

```bash
cd source/consul && go test -v ./...
```

---

## 関連ドキュメント

- [Source ドキュメントに戻る](../README_JP.md)
- [メインドキュメントに戻る](../../README_JP.md)

## ライセンス

[MIT License](../../LICENSE)
