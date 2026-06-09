# JavaScript スクリプトエンジン

[goja](https://github.com/dop251/goja) をベースにした Go 組み込み JavaScript スクリプトエンジン実装。[`go-scripts`](../) のサブモジュールとして提供。

## 設計要点

- **統一インターフェース**：[`script_engine.Engine`](../engine.go) インターフェースを実装し、[`Manager`](../manager.go)、[`EnginePool`](../engine_pool.go)、[`AutoGrowEnginePool`](../engine_pool_autogrow.go) などのルートモジュールコンポーネントとシームレスに連携可能。
- **ファクトリ自動登録**：`init()` で [`script_engine`](../factory.go) グローバルファクトリテーブルに登録。`import _ "github.com/tx7do/go-scripts/javascript"` するだけで有効化。
- **並行安全性**：内部で `sync.RWMutex` + `execMu` のダブルロックにより runtime / programs / source を保護。`Execute*` 系は `goja.Runtime.Interrupt` で `ctx.Done()` 中断をサポート。
- **スクリプトソースの分離**：`Source` インターフェース（`FileSource` / `MemSource` / `MultiSource` / カスタム拡張）でスクリプトソースを注入。engine 自体は IO の詳細に結合しない。
- **Node.js 互換レイヤー**：組み込みで `require` / `console` / `process` の goja_nodejs モジュールを有効化。CommonJS スタイルのモジュール読み込みがそのまま使用可能。
- **ES6 サブセットサポート**：goja は ES5 全部 + ES6 の一部（`let` / `const` / テンプレート文字列 / アロー関数 / 分割代入など）をサポート。

## 依存関係

- Go 1.24+
- [`github.com/dop251/goja`](https://github.com/dop251/goja)
- [`github.com/dop251/goja_nodejs`](https://github.com/dop251/goja_nodejs)（`require` / `console` / `process` を提供）
- [`github.com/tx7do/go-scripts`](../)（ルートモジュール）

## クイックスタート

### 1. インポート

```go
import (
    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/javascript" // JavaScript ファクトリを登録
)
```

`init()` がトリガーされると、以降のすべての `scriptEngine.NewScriptEngine(scriptEngine.JavaScriptType, ...)` 呼び出しで本モジュールの engine インスタンスが返されます。

### 2. 単一インスタンス使用

```go
eng, err := scriptEngine.NewScriptEngine(scriptEngine.JavaScriptType)
if err != nil {
    log.Fatal(err)
}
defer eng.Close()

ctx := context.Background()
if err := eng.Init(ctx); err != nil {
    log.Fatal(err)
}

// 変数注入（host -> JS）
_ = eng.RegisterGlobal("answer", 42)

// 関数注入（host -> JS）
_ = eng.RegisterFunction("sayHello", func(name string) {
    fmt.Println("Hello,", name)
})

// インラインスクリプト実行
result, err := eng.ExecuteString(ctx, "demo.js", `
    sayHello("world");
    answer + 100;
`)
// result = 142
```

### 3. エンジンプール（本番推奨）

```go
// 固定サイズプール
pool, err := scriptEngine.NewEnginePool(8, scriptEngine.JavaScriptType)

// または：自動拡張プール（初期 2、上限 16）
pool, err := scriptEngine.NewAutoGrowEnginePool(2, 16, scriptEngine.JavaScriptType)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

ctx := context.Background()
_, _ = pool.ExecuteString(ctx, "init.js", `globalThis.appName = "demo";`)
```

### 4. ScriptSource との組み合わせ（統一スクリプトソース）

```go
// ローカルファイル + mtime ホットリロード検出
src := scriptEngine.NewFileSource()
eng.SetSource(src)

// Source からロードして実行
result, err := eng.ExecuteFromKey(ctx, "/path/to/script.js")

// engine pool の wrapper 経由も可能
pool.SetSource(src)
results, err := pool.ExecuteFromKeys(ctx, []string{"a.js", "b.js"})
```

`MemSource`（純メモリ、IO ゼロ）や `MultiSource`（マルチソース集約 / fallback）も使用可能。

## コア API

`Engine` インターフェースが提供するメソッド（抜粋、完全な定義は [`engine.go`](../engine.go) を参照）：

### ライフサイクル

| メソッド | 説明 |
|---|---|
| `Init(ctx)` | runtime を初期化。任意の Load*/Execute* の前に呼び出す必要あり |
| `Close()` | runtime を解放。再利用には再 Init が必要 |
| `IsInitialized()` | 初期化状態を照会 |

### ScriptSource

| メソッド | 説明 |
|---|---|
| `SetSource(source)` | スクリプトソースをバインド（FileSource / S3 / Mem / Multi / ...）。`nil` でクリア |
| `GetSource()` | 現在バインドされている Source を取得（未バインドなら `nil`） |

### スクリプトロード

| メソッド | 説明 |
|---|---|
| `Load(ctx, key)` | バインドされた Source から単一スクリプトをロード（`key` はパス / object key / スクリプト ID） |
| `LoadMulti(ctx, keys)` | バッチロード。最初のエラーで中断 |
| `LoadString(ctx, name, code)` | インラインスクリプトを直接コンパイル、**Source を経由しない**。`name` は診断用（スタックトレースに表示） |

### スクリプト実行

| メソッド | 説明 |
|---|---|
| `Execute(ctx)` | `Load*` 済みのすべてのスクリプトを実行し、順序通りに結果を返す |
| `ExecuteFromKey(ctx, key)` | Source からロード + 即時実行 |
| `ExecuteFromKeys(ctx, keys)` | 複数 key 版。結果順序は `keys` と一致 |
| `ExecuteString(ctx, name, code)` | インラインスクリプトをコンパイルして即時実行、**Source を経由しない** |

### グローバル変数 / 関数 / モジュール

| メソッド | 説明 |
|---|---|
| `RegisterGlobal(name, value)` | JS グローバル変数を登録または上書き（任意の Go 値、goja が自動変換） |
| `GetGlobal(name)` | JS グローバル変数を読み取り。未定義の場合は error を返す |
| `RegisterFunction(name, fn)` | ホスト関数を登録。スクリプトは `name` で直接呼び出し可能 |
| `CallFunction(ctx, name, args...)` | `name` というスクリプト関数を呼び出し。`ctx` のタイムアウト / 中断をサポート |
| `RegisterModule(name, module)` | モジュールを登録。`module` は `map[string]any`（複数メンバーをエクスポート）または単一値 |

### エラー処理

| メソッド | 説明 |
|---|---|
| `GetLastError()` | 直近に発生したエラーを取得 |
| `ClearError()` | 直近のエラー状態をクリア |

## ホスト ↔ スクリプトの相互運用

### 変数

3 つのアクセスモードをサポート：

- **書き込み専用**：`RegisterGlobal` でホスト変数をスクリプトに注入。
- **読み取り専用**：`GetGlobal` でスクリプト内の変数を読み取り。
- **双方向読み書き**：ポインタ / 構造体参照を渡すと、スクリプトによるフィールド変更がホストに反映（goja が自動ブリッジ）。

### 関数

- **スクリプトからホストを呼ぶ**：`RegisterFunction` 後、スクリプト内で名前で直接呼び出し可能。
- **ホストからスクリプトを呼ぶ**：まず `ExecuteString` で関数を定義（例：`function add(a,b){return a+b}`）、その後 `CallFunction(ctx, "add", 1, 2)`。

### モジュール

`RegisterModule(name, module)` は関連する変数 / 関数を 1 つの名前空間にカプセル化し、グローバルスタックの汚染を防ぎます：

```go
_ = eng.RegisterModule("mathutil", map[string]any{
    "square": func(x float64) float64 { return x * x },
    "pi":     3.14159,
})
```

```javascript
// スクリプト側
mathutil.square(mathutil.pi * 2)
```

### CommonJS モジュール（`require`）

本モジュールは初期化時に `goja_nodejs` の `require` / `console` / `process` を有効化するため、スクリプト内で標準的な Node.js スタイルのモジュールが使用可能：

```javascript
// script/m.js
function sayHi(user) {
    console.log(`Js module say Hello, ${user}!`);
}
module.exports = { sayHi: sayHi };
```

```javascript
// script/main.js
var m = require("./script/m.js");
m.sayHi("Tom");
```

`require` はデフォルトで現在のワーキングディレクトリまたはスクリプトのディレクトリからの相対パスで解決します。[goja_nodejs require](https://github.com/dop251/goja_nodejs) を参照。

## 完全なサンプル

```go
package main

import (
    "context"
    "fmt"
    "log"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/javascript"
)

type User struct {
    Name  string
    Token string
}

func main() {
    pool, err := scriptEngine.NewEnginePool(4, scriptEngine.JavaScriptType)
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    ctx := context.Background()

    // 1. 構造体を注入（スクリプトはフィールドを変更可能）
    u := &User{Name: "Tim"}
    _ = pool.RegisterGlobal("u", u)

    // 2. ホスト関数を注入
    _ = pool.RegisterFunction("sayHello", func(name string) {
        fmt.Println("Hello,", name)
    })

    // 3. モジュールを注入
    _ = pool.RegisterModule("config", map[string]any{
        "env":  "prod",
        "port": 8080,
    })

    // 4. インラインスクリプトを実行
    _, err = pool.ExecuteString(ctx, "app.js", `
        sayHello(u.Name);
        u.Token = "abcd-1234";
        console.log("env:", config.env, "port:", config.port);
    `)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("user token:", u.Token) // abcd-1234
}
```

## テスト

```bash
cd javascript
go test -v ./...
```

カバレッジ：

- 基本実行 + グローバル変数読み書き + ホスト関数注入
- 並行 `ExecuteString` / `CallFunction` ストレステスト
- 並行 `Init` / `Close` / `Execute` ストレステスト
- `Source` 注入 + `Load` / `LoadMulti` / `ExecuteFromKey` / `ExecuteFromKeys`
- `FileSource` エンドツーエンド（`t.TempDir()` + 一時スクリプトファイル）

## 関連ドキュメント

- ルートモジュール README：[../README_JP.md](../README_JP.md)
- Engine インターフェース定義：[../engine.go](../engine.go)
- ScriptSource 実装：[../source.go](../source.go)
- エンジンプール：[../engine_pool.go](../engine_pool.go) / [../engine_pool_autogrow.go](../engine_pool_autogrow.go)
- goja ドキュメント：https://github.com/dop251/goja
