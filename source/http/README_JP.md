# HTTP Source · HTTP スクリプトソース

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**HTTP(S) ベースのリモートスクリプトソース、CRC32 チェックサムホットリロード検出対応**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

HTTP Source は HTTP(S) プロトコルを介して Web サーバー、CDN、REST API、または HTTP アクセス可能な任意のリソースからスクリプトを取得します。レスポンスボディの CRC32 チェックサムを比較することでホットリロードを検出します。

### 特徴

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `net/http`（標準ライブラリ） |
| ホットリロード | CRC32 チェックサム比較ポーリング |
| カスタムヘッダー | `WithHeader` で認証などを設定可能 |
| ベース URL | `WithBaseURL` + key で完全 URL を構成 |
| キー接頭辞 | `WithPrefix` でパスプレフィックスを設定 |
| インターフェース | `source.ReadWatcher` を実装 |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/source/http
```

---

## 設定オプション

| オプション | デフォルト | 説明 |
| --- | --- | --- |
| `WithBaseURL(url)` | **必須** | ベース URL、key が後に追加される |
| `WithPrefix(prefix)` | 空 | キー接頭辞（先頭の `/` は削除） |
| `WithTimeout(d)` | 30s | HTTP クライアントタイムアウト |
| `WithHeader(k, v)` | 空 | カスタムヘッダー（複数回呼び出し可能） |
| `WithHTTPClient(c)` | デフォルト Client | カスタム `*http.Client` を注入 |

---

## クイックスタート

```go
package main

import (
    "context"
    "fmt"
    "time"
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

    // スクリプトをロード（URL: https://api.example.com/scripts/hello.lua）
    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

---

## テスト

```bash
cd source/http && go test -v ./...
```

---

## 関連ドキュメント

- [Source ドキュメントに戻る](../README_JP.md)
- [メインドキュメントに戻る](../../README_JP.md)

## ライセンス

[MIT License](../../LICENSE)
