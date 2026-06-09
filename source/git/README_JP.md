# Git Source · Git スクリプトソース

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Git リポジトリベースのスクリプトソース、コミットハッシュ比較ホットリロード検出対応**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Git Source は [go-git](https://github.com/go-git/go-git) を使用してリモート Git リポジトリまたはローカル Git リポジトリからスクリプトを読み取ります。初期化時にリポジトリをローカルの一時ディレクトリに自動的にクローンします。コミットハッシュのポーリングによりホットリロードを検出します。

### 特徴

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/go-git/go-git/v5`（純 Go、CGO ゼロ） |
| ホットリロード | コミットハッシュ比較ポーリング（`git pull`） |
| 認証 | トークン / ユーザー名+パスワード / SSH 鍵 |
| シャロークロー | `WithDepth` でクローン深度を制限 |
| ローカルリポジトリ | `WithLocalPath` でローカルリポジトリを直接開く |
| キー接頭辞 | `WithPrefix` で名前空間を分離 |
| インターフェース | `source.ReadWatcher` を実装 |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/source/git
```

---

## 設定オプション

| オプション | デフォルト | 説明 |
| --- | --- | --- |
| `WithRepoURL(url)` | **必須*** | リモート Git リポジトリ URL |
| `WithBranch(branch)` | `HEAD` | ブランチ / タグ / 参照名 |
| `WithPrefix(prefix)` | 空 | キー接頭辞（先頭の `/` は削除） |
| `WithLocalPath(path)` | 空 | ローカルリポジトリパス（RepoURL の代替） |
| `WithAuth(user, pass)` | 空 | ユーザー名 + パスワード認証 |
| `WithToken(token)` | 空 | Bearer トークン（GitHub PAT / GitLab Token） |
| `WithSSHKey(path)` | 空 | SSH 秘密鍵ファイルパス |
| `WithDepth(n)` | 0（完全クローン） | シャロークロー深度 |
| `WithPullInterval(d)` | 30s | Watch ポーリング間隔 |

> \* `WithLocalPath` 使用時は `WithRepoURL` は不要です。

---

## クイックスタート

### リモートリポジトリからロード

```go
package main

import (
    "context"
    "fmt"
    gitSrc "github.com/tx7do/go-scripts/source/git"
)

func main() {
    ctx := context.Background()

    src, err := gitSrc.New(ctx,
        gitSrc.WithRepoURL("https://github.com/user/scripts.git"),
        gitSrc.WithBranch("main"),
        gitSrc.WithPrefix("scripts/lua/"),
    )
    if err != nil { panic(err) }
    defer src.Close()

    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

### トークン認証（GitHub）

```go
src, err := gitSrc.New(ctx,
    gitSrc.WithRepoURL("https://github.com/my-org/scripts.git"),
    gitSrc.WithToken("ghp_xxxxxxxxxxxx"),
    gitSrc.WithDepth(1),  // シャロークロー
)
```

### ローカルリポジトリ

```go
src, err := gitSrc.New(ctx,
    gitSrc.WithLocalPath("/path/to/local/repo"),
    gitSrc.WithPrefix("lua/"),
)
```

### ホットリロード監視

```go
// ベースライン確立のため先に Load
_, _ = src.Load(ctx, "main.lua")

// 監視開始（定期的に git pull + HEAD ハッシュ比較）
ch, _ := src.Watch(ctx, "main.lua")

for range ch {
    // リモートリポジトリに新規コミットあり
    code, _ := src.Load(ctx, "main.lua")
    fmt.Println("reloaded:", code)
}
```

---

## ホットリロードメカニズム

```
30s ごと（デフォルト）:
  git pull
    ↓
  現在の HEAD コミットハッシュを取得
    ↓
  Load 時に記録したベースラインハッシュと比較
    ↓
  差異あり -> 変更シグナルを送信
```

- **各 Watcher が独立して追跡**：各 `Watch()` 呼び出しは独自のベースラインを維持し、複数の Watcher が互いに干渉しません。
- **Pull 失敗時のフォールバック**：ネットワーク異常などで Pull が失敗した場合、現在のティックをスキップし、次のインターバルで再試行します。
- **変更なしはシグナルなし**：HEAD ハッシュが変化していない場合はシグナルを送信しません。

---

## エラー処理

| エラー | 説明 |
| --- | --- |
| `ErrNotFound` | ファイルが存在しない。`errors.Is(err, gitSrc.ErrNotFound)` または `gitSrc.IsNotFound(err)` で識別 |
| その他 | 元のエラーを `git source: ...` プレフィックス付きでラップ |

---

## テスト

```bash
cd source/git && go test -v ./...
```

テストカバレッジ（25 ケース、すべて成功、実際の Git サーバー不要）：

| カテゴリ | ケース |
| --- | --- |
| インターフェース実装 | コンパイル時アサーション |
| 構築バリデーション | `WithRepoURL` または `WithLocalPath` 必須 |
| 接頭辞正規化 | 6 種類の prefix 書き方テーブル駆動テスト |
| Load | 正常 / 未検出（ErrNotFound）/ prefix 付き / Context キャンセル |
| Watch | 変更検出 / 変更なし / Context キャンセル / 未 Load エラー / Pull 失敗フォールバック / 並行複数 Watcher |
| 並行安全性 | 30 goroutine 並行 Load |
| オプション | WithRepoURL / WithBranch / WithAuth / WithToken / WithDepth / WithPullInterval |
| ローカルリポジトリ | ローカルディレクトリからロード |

---

## 関連ドキュメント

- [Source ドキュメントに戻る](../README_JP.md)
- [メインドキュメントに戻る](../../README_JP.md)

## ライセンス

[MIT License](../../LICENSE)
