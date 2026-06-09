# TCL エンジン · TCL スクリプトエンジン

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-TclType-blue)](../types.go)

**[modernc.org/tcl](https://pkg.go.dev/modernc.org/tcl) ベースの 100% 互換 Tcl エンジン**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

TCL エンジンは [modernc.org/tcl](https://pkg.go.dev/modernc.org/tcl) ライブラリ —— CGO フリーの Tcl ポートを使用します。Go アプリケーションに Tool Command Language の完全なスクリプト実行機能を提供し、レガシーシステム統合やネットワーク機器スクリプトに適しています。

### エンジン特性

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `modernc.org/tcl` v1.15.3 |
| 言語バージョン | TCL 8.6（100% 互換） |
| CGO 依存 | なし（ピュア Go ポート） |
| 自動マウント | 起動時に TCL ライブラリ VFS を自動マウント |
| スレッドセーフ | デュアルロックパターン（`RWMutex` + `execMutex`） |
| ホットリロード | `StartWatch` / `StopWatch` 対応 |
| タイプ定数 | `scriptEngine.TclType` |

> **プラットフォーム注意**: Windows では、`modernc.org/tcl` の `Close()` で notifier デッドロックが発生する可能性があります。エンジンは Go 側の状態を正しくクリーンアップしますが、基盤となるインタープリタの破棄はプロセス終了に依存します。

---

## インストール

```bash
go get github.com/tx7do/go-scripts/tcl
```

---

## クイックスタート

```go
package main

import (
    "context"
    "fmt"
    "log"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/tcl" // TCL エンジンを登録
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.TclType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    result, _ := eng.ExecuteString(ctx, "hello.tcl", `greet $name`)
    fmt.Println(result) // Hello, world!
}
```

---

## API リファレンス

| メソッド | 説明 |
| --- | --- |
| `Init(ctx)` | TCL インタープリタを作成、ライブラリ VFS をマウント |
| `Close()` | エンジン状態をクリーンアップ（Go 側） |
| `LoadString(ctx, name, code)` | TCL ソースを実行キューに追加 |
| `Execute(ctx)` | キュー内のすべてのスクリプトを順次実行、インタープリタを共有 |
| `ExecuteString(ctx, name, code)` | インライン TCL コードを即座に実行 |
| `RegisterGlobal(name, value)` | `set` コマンドで TCL 変数を設定 |
| `RegisterFunction(name, fn)` | Go 関数を TCL コマンドとして登録 |
| `CallFunction(ctx, name, args...)` | TCL プロシージャまたは登録済みコマンドを呼び出し |
| `RegisterModule(name, module)` | モジュールを登録（キーは `モジュール名_キー名` にマッピング） |
| `StartWatch(ctx, key)` | スクリプトのホットリロード監視を開始 |
| `StopWatch(key)` | ホットリロード監視を停止 |

---

## テスト

```bash
cd tcl && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README.md)
- [modernc.org/tcl ドキュメント](https://pkg.go.dev/modernc.org/tcl)
- [TCL 公式ウェブサイト](https://tcl.tk)

## ライセンス

[MIT License](../LICENSE)
