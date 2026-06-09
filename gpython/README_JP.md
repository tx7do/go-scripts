# GPython エンジン · Python スクリプトエンジン

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-GPythonType-blue)](../types.go)

**[gpython](https://github.com/go-python/gpython) ベースのピュア Go Python 3.4 サブセットスクリプトエンジン**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

GPython エンジンは [go-python/gpython](https://github.com/go-python/gpython) ライブラリを使用し、Go アプリケーションに Python 3.4 サブセットのスクリプト実行機能を提供します。CPython 依存なし、ピュア Go 実装で、クロスプラットフォームコンパイルが容易です。

### エンジン特性

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/go-python/gpython` v0.2.0 |
| 言語バージョン | Python 3.4 サブセット |
| 標準ライブラリ | 組み込み `sys`、`time`、`math` などのネイティブモジュール |
| スレッドセーフ | デュアルロックパターン（`RWMutex` + `execMutex`） |
| ホットリロード | `StartWatch` / `StopWatch` 対応 |
| タイプ定数 | `scriptEngine.GPythonType` |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/gpython
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
    _ "github.com/tx7do/go-scripts/gpython" // GPython エンジンを登録
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.GPythonType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    if err := eng.Init(ctx); err != nil {
        log.Fatal(err)
    }

    // ホスト変数を注入
    _ = eng.RegisterGlobal("name", "world")

    // ホスト関数を登録
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // Python スクリプトを実行
    result, _ := eng.ExecuteString(ctx, "hello.py", `greet(name)`)
    fmt.Println(result)
}
```

---

## API リファレンス

| メソッド | 説明 |
| --- | --- |
| `Init(ctx)` | gpython インタープリタコンテキストとメインモジュールを作成 |
| `Close()` | インタープリタを閉じ、リソースを解放 |
| `LoadString(ctx, name, code)` | Python ソースをコンパイルし実行キューに追加 |
| `Execute(ctx)` | キュー内のすべてのスクリプトを順次実行 |
| `ExecuteString(ctx, name, code)` | インライン Python コードをコンパイルして即座に実行 |
| `RegisterGlobal(name, value)` | Go の値を Python グローバル変数として注入 |
| `RegisterFunction(name, fn)` | Go 関数を Python 呼び出し可能オブジェクトとして登録 |
| `CallFunction(ctx, name, args...)` | Python 関数を呼び出し |
| `RegisterModule(name, module)` | モジュールを登録（キーは `モジュール名_キー名` グローバル変数としてマッピング） |
| `StartWatch(ctx, key)` | スクリプトのホットリロード監視を開始 |
| `StopWatch(key)` | ホットリロード監視を停止 |

---

## テスト

```bash
cd gpython && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README.md)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/gpython)

## ライセンス

[MIT License](../LICENSE)
