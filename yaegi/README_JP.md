# Yaegi エンジン · Go スクリプトエンジン

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-YaegiType-blue)](../types.go)

**[Traefik Yaegi](https://github.com/traefik/yaegi) ベースのネイティブ Go スクリプトエンジン**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Yaegi エンジンは [Traefik Yaegi](https://github.com/traefik/yaegi) ライブラリを使用し、Go アプリケーションにネイティブ Go 言語のランタイムスクリプト実行機能を提供します。コンパイル不要、Go ソースコードを直接解釈実行します。ダイナミックプラグインや DevOps ツールチェーンに最適です。

### エンジン特性

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | `github.com/traefik/yaegi` v0.16.1 |
| 言語バージョン | Go 1.x |
| ホスト相互運用 | 合成 `host` パッケージ経由でグローバル変数と関数を公開、スクリプトは `import "host"` が必要 |
| スレッドセーフ | デュアルロックパターン（`RWMutex` + `execMutex`） |
| ホットリロード | `StartWatch` / `StopWatch` 対応 |
| タイプ定数 | `scriptEngine.YaegiType` |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/yaegi
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
    _ "github.com/tx7do/go-scripts/yaegi" // Yaegi エンジンを登録
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.YaegiType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // ホスト関数を登録（スクリプトは host.FuncName で呼び出し）
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // Go スクリプトを実行（注意: import "host" が必要）
    result, _ := eng.ExecuteString(ctx, "hello.go", `
        import "host"
        host.greet("world")
    `)
    fmt.Println(result) // Hello, world!
}
```

> **注意**: Yaegi エンジンでは、`RegisterGlobal` / `RegisterFunction` で登録された変数と関数は合成 `host` パッケージに配置されます。スクリプトは `import "host"` が必要です。

---

## API リファレンス

| メソッド | 説明 |
| --- | --- |
| `Init(ctx)` | Yaegi インタープリタインスタンスを作成 |
| `Close()` | インタープリタを閉じ、リソースを解放 |
| `LoadString(ctx, name, code)` | Go ソースコードを実行キューに追加 |
| `Execute(ctx)` | キュー内のすべてのスクリプトを順次実行 |
| `ExecuteString(ctx, name, code)` | インライン Go コードをコンパイルして即座に実行 |
| `RegisterGlobal(name, value)` | `host` パッケージにグローバル変数を登録 |
| `RegisterFunction(name, fn)` | `host` パッケージに Go 関数を登録 |
| `CallFunction(ctx, name, args...)` | 登録済みの関数を呼び出し |
| `RegisterModule(name, module)` | スクリプトが `import` できるカスタムパッケージを登録 |
| `StartWatch(ctx, key)` | スクリプトのホットリロード監視を開始 |
| `StopWatch(key)` | ホットリロード監視を停止 |

---

## テスト

```bash
cd yaegi && go test -v ./...
```

---

## 関連ドキュメント

- [メインドキュメントに戻る](../README.md)
- [Yaegi 公式ドキュメント](https://github.com/traefik/yaegi)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/yaegi)

## ライセンス

[MIT License](../LICENSE)
