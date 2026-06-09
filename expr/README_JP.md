# Expr エンジン · 式エンジン

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-ExprType-blue)](../types.go)

**[expr-lang/expr](https://github.com/expr-lang/expr) ベースの軽量式エンジン**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Expr エンジンは [expr-lang/expr](https://github.com/expr-lang/expr) ライブラリを使用し、高速で型安全な式評価を提供します。変数、関数、豊富な組み込み演算子をサポートし、ビジネス式、テンプレートエンジン、データフィルタリングに最適です。

### エンジン特性

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/expr-lang/expr` v1.17.0 |
| 言語 | Expr DSL |
| 型システム | ダックタイピング、コンパイル時型チェック |
| 環境スナップショット | 各コンパイル/評価時に環境スナップショットを作成し一貫性を保証 |
| スレッドセーフ | デュアルロックパターン（`RWMutex` + `execMutex`） |
| ホットリロード | `StartWatch` / `StopWatch` 対応 |
| タイプ定数 | `scriptEngine.ExprType` |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/expr
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
    _ "github.com/tx7do/go-scripts/expr" // Expr エンジンを登録
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.ExprType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterGlobal("items", []int{1, 2, 3, 4, 5})
    _ = eng.RegisterFunction("double", func(x int) int { return x * 2 })

    result, _ := eng.ExecuteString(ctx, "filter", `items | filter(# > 2) | map(double(#))`)
    fmt.Println(result) // [6, 8, 10]
}
```

---

## API リファレンス

| メソッド | 説明 |
| --- | --- |
| `Init(ctx)` | エンジン環境を初期化 |
| `Close()` | エンジンリソースを解放 |
| `LoadString(ctx, name, code)` | Expr 式をコンパイルしてキューに追加 |
| `Execute(ctx)` | キュー内のすべての式を順次評価、最後の結果を返す |
| `ExecuteString(ctx, name, code)` | インライン Expr 式をコンパイルして即座に評価 |
| `RegisterGlobal(name, value)` | 式環境にグローバル変数を登録 |
| `RegisterFunction(name, fn)` | 式環境に Go 関数を登録 |
| `CallFunction(ctx, name, args...)` | 登録済みホスト関数を命令的に呼び出し |
| `RegisterModule(name, module)` | 名前空間モジュールを登録 |
| `StartWatch(ctx, key)` | 式のホットリロード監視を開始 |
| `StopWatch(key)` | ホットリロード監視を停止 |

---

## テスト

```bash
cd expr && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README.md)
- [Expr 公式ドキュメント](https://github.com/expr-lang/expr)
- [Expr 言語リファレンス](https://expr-lang.org/docs/language-definition)

## ライセンス

[MIT License](../LICENSE)
