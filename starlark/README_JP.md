# Starlark エンジン · Python 方言スクリプトエンジン

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-StarlarkType-blue)](../types.go)

**[google/starlark-go](https://github.com/google/starlark-go) ベースのセキュア組み込みスクリプトエンジン**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Starlark エンジンは Google の [starlark-go](https://github.com/google/starlark-go) ライブラリを使用します。Starlark は組み込みスクリプト用に設計された Python の方言で、決定論的実行、スレッドセーフ、サンドボックス化の特徴を持ちます。Bazel ビルドシステムなどで広く使用されています。

### エンジン特性

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `go.starlark.net` |
| 言語 | Starlark（Python 方言） |
| セキュリティ | 決定論的実行、副作用のないサンドボックス |
| グローバル変数 | `hostPredeclared` + `scriptGlobals` の二層名前空間 |
| スレッドセーフ | デュアルロックパターン（`RWMutex` + `execMutex`） |
| ホットリロード | `StartWatch` / `StopWatch` 対応 |
| タイプ定数 | `scriptEngine.StarlarkType` |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/starlark
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
    _ "github.com/tx7do/go-scripts/starlark" // Starlark エンジンを登録
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.StarlarkType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    result, _ := eng.ExecuteString(ctx, "hello.star", `greet(name)`)
    fmt.Println(result) // Hello, world!
}
```

---

## API リファレンス

| メソッド | 説明 |
| --- | --- |
| `Init(ctx)` | Starlark エンジンを初期化 |
| `Close()` | エンジンリソースを解放 |
| `LoadString(ctx, name, code)` | Starlark ソースを実行キューに追加 |
| `Execute(ctx)` | キュー内のすべてのスクリプトを順次実行、グローバル変数を共有 |
| `ExecuteString(ctx, name, code)` | インライン Starlark コードをコンパイルして即座に実行 |
| `RegisterGlobal(name, value)` | `hostPredeclared` に変数を登録 |
| `RegisterFunction(name, fn)` | Go 関数を Starlark `Builtin` としてラップ |
| `CallFunction(ctx, name, args...)` | Starlark 関数または登録済みホスト関数を呼び出し |
| `RegisterModule(name, module)` | モジュールを登録（キーは `モジュール名_キー名` にマッピング） |
| `StartWatch(ctx, key)` | スクリプトのホットリロード監視を開始 |
| `StopWatch(key)` | ホットリロード監視を停止 |

---

## テスト

```bash
cd starlark && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README.md)
- [Starlark 公式ドキュメント](https://github.com/google/starlark-go)
- [Starlark 言語仕様](https://docs.bazel.build/versions/main/skylark/language.html)

## ライセンス

[MIT License](../LICENSE)
