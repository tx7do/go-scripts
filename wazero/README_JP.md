# Wazero エンジン · WebAssembly スクリプトエンジン

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-WazeroType-blue)](../types.go)

**[wazero](https://github.com/tetratelabs/wazero) ベースのゼロ CGO WebAssembly ランタイムエンジン**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Wazero エンジンは [tetratelabs/wazero](https://github.com/tetratelabs/wazero) ライブラリを使用し、Go アプリケーションに WebAssembly (WASM) モジュールの読み込みと実行機能を提供します。WASM はバイナリ形式のため、`LoadString` / `ExecuteString` の `code` パラメータは生の WASM バイトです。

### エンジン特性

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/tetratelabs/wazero` v1.9.0 |
| フォーマット | WASM 1.0 バイナリ |
| ホスト関数 | `host` インポートモジュール経由で公開、WASM モジュールは `import "host"` で利用可能 |
| 関数呼び出し | `CallFunction` はエクスポートされた WASM 関数を `uint64` 引数で呼び出し |
| スレッドセーフ | デュアルロックパターン（`RWMutex` + `execMutex`） |
| ホットリロード | `StartWatch` / `StopWatch` 対応 |
| タイプ定数 | `scriptEngine.WazeroType` |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/wazero
```

---

## クイックスタート

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/wazero" // Wazero エンジンを登録
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.WazeroType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // WASM モジュールを読み込み（生バイト）
    wasmBytes, _ := os.ReadFile("add.wasm")
    _ = eng.LoadString(ctx, "add.wasm", string(wasmBytes))

    // インスタンス化して実行（_start を呼び出し）
    eng.Execute(ctx)

    // エクスポートされた WASM 関数を呼び出し
    result, _ := eng.CallFunction(ctx, "add", uint64(3), uint64(4))
    fmt.Println(result) // 7
}
```

---

## API リファレンス

| メソッド | 説明 |
| --- | --- |
| `Init(ctx)` | wazero ランタイムを作成 |
| `Close()` | ランタイムとすべてのコンパイル済みモジュールを閉じる |
| `LoadString(ctx, name, code)` | WASM バイトをコンパイルしてキューに追加 |
| `Execute(ctx)` | 最後のモジュールをインスタンス化し `_start` を呼び出し |
| `ExecuteString(ctx, name, code)` | WASM バイトをコンパイルして即座にインスタンス化 |
| `RegisterFunction(name, fn)` | `host` インポートモジュールにホスト関数を登録 |
| `CallFunction(ctx, name, args...)` | エクスポートされた WASM 関数を呼び出し（引数は `uint64`） |
| `RegisterModule(name, module)` | 名前付きホストモジュールを登録 |
| `GetGlobal(name)` | WASM エクスポートグローバルを読み取り（`uint64` を返す） |
| `StartWatch(ctx, key)` | WASM モジュールのホットリロード監視を開始 |
| `StopWatch(key)` | ホットリロード監視を停止 |

> **注意**: `RegisterFunction` で登録するホスト関数のパラメータは、WASM 呼び出し規約に互換である必要があります（`context.Context`、`api.Module`、または数値型 `uint32/uint64/int32/int64/float32/float64`）。

---

## テスト

```bash
cd wazero && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README.md)
- [wazero 公式ドキュメント](https://github.com/tetratelabs/wazero)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/wazero)

## ライセンス

[MIT License](../LICENSE)
