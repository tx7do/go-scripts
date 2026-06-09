# CEL エンジン · 式エンジン

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-CELType-blue)](../types.go)

**[cel-go](https://github.com/google/cel-go) ベースの Google Common Expression Language エンジン**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

CEL エンジンは Google の [cel-go](https://github.com/google/cel-go) ライブラリを使用し、CEL (Common Expression Language) 式のコンパイルと評価を提供します。CEL は非チューリング完全の式言語で、シンプルさ、安全性、高速な評価のために設計されています。各「スクリプト」は単一の式を評価し、コンパイル時の型チェックをサポートします。

### エンジン特性

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/google/cel-go` v0.22.1 |
| 言語 | CEL 仕様（非チューリング完全の式言語） |
| 型推論 | リフレクションによる Go 値からの CEL 型自動推論 |
| 環境再構築 | グローバル変数/関数の登録時に CEL 環境を自動再構築 |
| スレッドセーフ | デュアルロックパターン（`RWMutex` + `execMutex`） |
| ホットリロード | `StartWatch` / `StopWatch` 対応 |
| タイプ定数 | `scriptEngine.CELType` |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/cel
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
    _ "github.com/tx7do/go-scripts/cel" // CEL エンジンを登録
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.CELType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterGlobal("age", 25)
    _ = eng.RegisterFunction("isAdult", func(age int) bool { return age >= 18 })

    result, _ := eng.ExecuteString(ctx, "check", `isAdult(age) && name == "world"`)
    fmt.Println(result) // true
}
```

---

## API リファレンス

| メソッド | 説明 |
| --- | --- |
| `Init(ctx)` | CEL 環境を作成 |
| `Close()` | エンジンリソースを解放 |
| `LoadString(ctx, name, code)` | CEL 式をコンパイルしてキューに追加 |
| `Execute(ctx)` | キュー内のすべての式を順次評価、最後の結果を返す |
| `ExecuteString(ctx, name, code)` | インライン CEL 式をコンパイルして即座に評価 |
| `RegisterGlobal(name, value)` | グローバル変数を登録、CEL 型を自動推論 |
| `RegisterFunction(name, fn)` | Go 関数を登録、型はリフレクションで推論 |
| `CallFunction(ctx, name, args...)` | 登録済みホスト関数を命令的に呼び出し |
| `RegisterModule(name, module)` | モジュールを登録（キーは `モジュール名_キー名` にマッピング） |
| `StartWatch(ctx, key)` | 式のホットリロード監視を開始 |
| `StopWatch(key)` | ホットリロード監視を停止 |

---

## テスト

```bash
cd cel && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README.md)
- [cel-go 公式ドキュメント](https://github.com/google/cel-go)
- [CEL 言語仕様](https://github.com/google/cel-spec)

## ライセンス

[MIT License](../LICENSE)
