# Lua スクリプトエンジン

[GopherLua](https://github.com/yuin/gopher-lua) をベースにした Go 組み込み Lua スクリプトエンジン実装。[`go-scripts`](../) のサブモジュールとして提供。

GopherLua は純粋な Go 実装の Lua 5.1 仮想マシンで、C スタイルの Lua C API に近く、性能も良く、CGO 依存もありません。

## 設計要点

- **統一インターフェース**：[`script_engine.Engine`](../engine.go) インターフェースを実装し、[`Manager`](../manager.go)、[`EnginePool`](../engine_pool.go)、[`AutoGrowEnginePool`](../engine_pool_autogrow.go) などのルートモジュールコンポーネントとシームレスに連携可能。
- **ファクトリ自動登録**：`init()` で [`script_engine`](../factory.go) グローバルファクトリテーブルに登録。`import _ "github.com/tx7do/go-scripts/lua"` するだけで有効化。
- **並行安全性**：内部で `sync.RWMutex` により VM / source / initialized を保護。`Execute*` / `CallFunction` は channel + `ctx.Done()` でキャンセルとタイムアウトをサポート。
- **LState 再利用プール**：本モジュール独自の [`statePool`](state_pool.go) があり、engine の `Init` 時に貸出、`Close` 時に返却。デフォルト上限 10 個の LState インスタンスで、毎回 VM を再構築するのを回避。
- **スクリプトソースの分離**：`Source` インターフェース（`FileSource` / `MemSource` / `MultiSource` / カスタム拡張）でスクリプトソースを注入。engine 自体は IO の詳細に結合しない。
- **標準 Lua エコシステム**：`Init` 時に Lua 標準ライブラリ + [`gopher-lua-libs`](https://github.com/vadv/gopher-lua-libs)（json / http / regexp / db / time / ...）+ [`gluacrypto`](https://github.com/tengattack/gluacrypto)（暗号 / ハッシュ）+ `GetLuaPath` ヘルパーを自動有効化。

## gopher-lua の制限事項（必読）

| 制限 | 挙動 | 対応戦略 |
|---|---|---|
| **単一コンパイルユニット** | LState は一度に 1 つのコンパイル済み `LFunction` のみ保持。連続した `Load` / `LoadString` は上書き | 複数スクリプトは `ExecuteFromKeys` または手動で `Load` + `Execute` をペアリング |
| **5.1 サブセット** | Lua 5.2+ の `goto` / bit32 / 5.3 整数型は非サポート | Lua 5.1 文法でスクリプトを記述 |
| **JIT なし** | LuaJIT より遅く、C Lua よりやや遅い | 高須度ホットパスは事前コンパイルまたは Go 側への移行を推奨 |
| **`LoadString` / `DoString` に name 引数なし** | 本モジュールの `LoadString(ctx, name, code)` / `ExecuteString(ctx, name, code)` の `name` はインターフェース互換用で**無視される** | スタックトレースにスクリプト名は表示されない |

## 依存関係

- Go 1.24+
- [`github.com/yuin/gopher-lua`](https://github.com/yuin/gopher-lua) —— Lua 5.1 仮想マシン
- [`layeh.com/gopher-luar`](https://github.com/layeh/gopher-luar) —— Go ↔ Lua 双方向値ブリッジ
- [`github.com/yuin/gluamapper`](https://github.com/yuin/gluamapper) —— Lua table ↔ Go struct マッピング
- [`github.com/vadv/gopher-lua-libs`](https://github.com/vadv/gopher-lua-libs) —— 汎用ライブラリ拡張（json/http/regexp/db/...）
- [`github.com/tengattack/gluacrypto`](https://github.com/tengattack/gluacrypto) —— 暗号 / ハッシュ拡張
- [`github.com/tx7do/go-scripts`](../) —— ルートモジュール

## クイックスタート

### 1. インポート

```go
import (
    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua" // Lua ファクトリを登録
)
```

`init()` がトリガーされると、以降のすべての `scriptEngine.NewScriptEngine(scriptEngine.LuaType, ...)` 呼び出しで本モジュールの engine インスタンスが返されます。

### 2. 単一インスタンス使用

```go
eng, err := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
if err != nil {
    log.Fatal(err)
}
defer eng.Close()

ctx := context.Background()
if err := eng.Init(ctx); err != nil {
    log.Fatal(err)
}

// 変数注入（host -> Lua）
_ = eng.RegisterGlobal("answer", 42)

// ホスト関数注入（Lua.LGFunction である必要あり）
_ = eng.RegisterFunction("say_hello", func(L *lua.LState) int {
    name := L.CheckString(1)
    fmt.Println("Hello,", name)
    return 0 // 戻り値の数
})

// インラインスクリプト実行
_, err = eng.ExecuteString(ctx, "demo.lua", `
    say_hello("world")
    print(answer + 100)
`)
```

### 3. エンジンプール（本番推奨）

```go
// 固定サイズプール
pool, err := scriptEngine.NewEnginePool(8, scriptEngine.LuaType)

// または：自動拡張プール（初期 2、上限 16）
pool, err := scriptEngine.NewAutoGrowEnginePool(2, 16, scriptEngine.LuaType)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

ctx := context.Background()
_, _ = pool.ExecuteString(ctx, "init.lua", `app_name = "demo"`)
```

### 4. ScriptSource との組み合わせ（統一スクリプトソース）

```go
// ローカルファイル + mtime ホットリロード検出
src := scriptEngine.NewFileSource()
eng.SetSource(src)

// Source からロードして実行
_, err := eng.ExecuteFromKey(ctx, "/path/to/script.lua")

// engine pool の wrapper 経由も可能
pool.SetSource(src)
results, err := pool.ExecuteFromKeys(ctx, []string{"a.lua", "b.lua"})
```

`MemSource`（純メモリ、IO ゼロ）や `MultiSource`（マルチソース集約 / fallback）も使用可能。

## コア API

`Engine` インターフェースが提供するメソッド（抜粋、完全な定義は [`engine.go`](../engine.go) を参照）：

### ライフサイクル

| メソッド | 説明 |
|---|---|
| `Init(ctx)` | `statePool` から LState を借用し、標準ライブラリを開く。任意の Load*/Execute* の前に呼び出す必要あり |
| `Close()` | LState をプールに返却。再利用には再 Init が必要 |
| `IsInitialized()` | 初期化状態を照会 |

### ScriptSource

| メソッド | 説明 |
|---|---|
| `SetSource(source)` | スクリプトソースをバインド（FileSource / S3 / Mem / Multi / ...）。`nil` でクリア |
| `GetSource()` | 現在バインドされている Source を取得（未バインドなら `nil`） |

### スクリプトロード

| メソッド | 説明 |
|---|---|
| `Load(ctx, key)` | バインドされた Source から単一スクリプトをロード。**注意**：gopher-lua は一度に 1 つのコンパイル済み関数のみ保持。連続 Load で上書き |
| `LoadMulti(ctx, keys)` | バッチロード。最初のエラーで中断。同じ上書きルール |
| `LoadString(ctx, name, code)` | インラインスクリプトを直接コンパイル、**Source を経由しない**。`name` は無視（gopher-lua 非サポート） |

### スクリプト実行

| メソッド | 説明 |
|---|---|
| `Execute(ctx)` | 最後の `Load*` でコンパイルされた関数を実行。ctx のキャンセルで channel 経由で中断 |
| `ExecuteFromKey(ctx, key)` | Source からロード + 即時実行 |
| `ExecuteFromKeys(ctx, keys)` | 複数 key 版。結果順序は `keys` と一致（推奨） |
| `ExecuteString(ctx, name, code)` | インラインスクリプトをコンパイルして即時実行、**Source を経由しない**。`name` は無視 |

### グローバル変数 / 関数 / モジュール

| メソッド | 説明 |
|---|---|
| `RegisterGlobal(name, value)` | Go 値を `luar` 経由で Lua グローバルにブリッジ。基本型、map、struct ポインタをサポート（**フィールドは双方向読み書き可能**） |
| `GetGlobal(name)` | Lua グローバル変数を読み取り、`interface{}` に自動変換（LNumber → int64/float64、LTable → map/slice） |
| `RegisterFunction(name, fn)` | Lua 関数を登録。`fn` は **`Lua.LGFunction` 型である必要あり**、それ以外は error を返す |
| `CallFunction(ctx, name, args...)` | `name` という Lua 関数を呼び出し。引数は LValue に自動変換、戻り値は Go 値に自動変換 |
| `RegisterModule(name, module)` | Lua モジュールを登録。`module` は **`Lua.LGFunction` 型である必要あり**（モジュール loader 関数） |

### エラー処理

| メソッド | 説明 |
|---|---|
| `GetLastError()` | 直近に発生したエラーを取得 |
| `ClearError()` | 直近のエラー状態をクリア |

## ホスト ↔ スクリプトの相互運用

### 変数

3 つのアクセスモードをサポート：

- **書き込み専用**：`RegisterGlobal` でホスト変数をスクリプトに注入（基本型 / map / slice）。
- **読み取り専用**：`GetGlobal` でスクリプト内の変数を読み取り。
- **双方向読み書き**：`luar` で**構造体ポインタ**を注入すると、スクリプトによるフィールド変更がホストに反映。

```go
type User struct {
    Name  string
    Token string
}

u := &User{Name: "Tim"}
_ = eng.RegisterGlobal("u", u)
_, _ = eng.ExecuteString(ctx, "", `u:SetToken("abcd")`)
fmt.Println(u.Token) // abcd
```

### 関数

**Lua 関数シグネチャ要件**：本モジュールの `RegisterFunction` / `RegisterModule` は **`Lua.LGFunction` のみ受け付け**（`func(*lua.LState) int`）。それ以外はエラーになります。

```go
import Lua "github.com/yuin/gopher-lua"

// host -> Lua
_ = eng.RegisterFunction("add", func(L *Lua.LState) int {
    a := L.CheckInt(1)
    b := L.CheckInt(2)
    L.Push(Lua.LNumber(a + b))
    return 1 // 戻り値の数
})

// Lua -> host
_, _ = eng.ExecuteString(ctx, "", `result = add(10, 20)`)
v, _ := eng.GetGlobal("result") // int64(30)
```

ホストからスクリプト内の関数を呼び出す：

```go
_, _ = eng.ExecuteString(ctx, "", `
    function multiply(a, b) return a * b end
`)
v, _ := eng.CallFunction(ctx, "multiply", 3, 4) // int64(12)
```

### モジュール

`RegisterModule` は Lua モジュール loader 関数（`Lua.LGFunction` 型）を登録：

```go
modLoader := func(L *Lua.LState) int {
    mod := L.NewTable()
    L.SetField(mod, "pi", Lua.LNumber(3.14159))
    L.SetField(mod, "square", L.NewFunction(func(L *Lua.LState) int {
        x := L.CheckNumber(1)
        L.Push(Lua.LNumber(x * x))
        return 1
    }))
    L.Push(mod)
    return 1
}
_ = eng.RegisterModule("mathutil", modLoader)
```

```lua
-- スクリプト側
print(mathutil.pi)               -- 3.14159
print(mathutil.square(2))        -- 4
```

> 注意：上記の `RegisterModule` は gopher-lua 組み込みの loader プロトコル（`L.NewFunction + L.Call`）を使用。`script/test_module.lua` で示している "`module` テーブル + `return module`" 書き方は **`require` ロード用**です。両者は競合せず、共存可能です。

### Lua 組み込みモジュール（自動有効化済み）

`virtualMachine.init()` で以下のライブラリを自動的に有効化：

| ライブラリ | ソース | 用途 |
|---|---|---|
| `string` / `table` / `math` / `io` / `os` / `debug` / `package` | gopher-lua 標準ライブラリ | Lua 5.1 組み込み |
| `json` | gopher-lua-libs | JSON エンコード/デコード |
| `http` | gopher-lua-libs | HTTP クライアント |
| `regexp` | gopher-lua-libs | 正規表現マッチング（Go regexp ベース） |
| `db` | gopher-lua-libs | MySQL / SQLite / PostgreSQL アクセス |
| `time` | gopher-lua-libs | 時間操作 |
| `crypto` | gluacrypto | md5 / sha / hmac / aes / des / rsa など |
| `GetLuaPath()` | 本モジュール独自 | 現在の実行ファイル配下の `script` サブディレクトリを返し、`package.path` の結合に使用 |

標準の `require` 書き方が使用可能：

```lua
-- script/test_module.lua
module = {}
module.constant = "これは定数です"
function module.func3() print("hello") end
return module

-- script/main.lua
local m = require("test_module")
print(m.constant)
m.func3()
```

## 完全なサンプル

```go
package main

import (
    "context"
    "fmt"
    "log"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua"
    Lua "github.com/yuin/gopher-lua"
)

type User struct {
    Name  string
    Token string
}

func main() {
    pool, err := scriptEngine.NewEnginePool(4, scriptEngine.LuaType)
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    ctx := context.Background()

    // 1. 構造体を注入（スクリプトはフィールドを変更可能）
    u := &User{Name: "Tim"}
    _ = pool.RegisterGlobal("u", u)

    // 2. ホスト関数を注入（Lua.LGFunction である必要あり）
    _ = pool.RegisterFunction("say_hello", func(L *Lua.LState) int {
        name := L.CheckString(1)
        fmt.Println("Hello,", name)
        return 0
    })

    // 3. インラインスクリプトを実行
    _, err = pool.ExecuteString(ctx, "app.lua", `
        say_hello(u.Name)
        u.Token = "abcd-1234"
        print("answer:", 6 * 7)
    `)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("user token:", u.Token) // abcd-1234
}
```

## LState プール

本モジュールは内部で [`statePool`](state_pool.go) を管理：

- `Borrow()` —— アイドル状態の LState を取得、なければ新規作成
- `Return(L)` —— LState を返却、上限超過時は `Close`
- `Shutdown()` —— プールをシャットダウンし、すべてのアイドル LState を回収（グローバルクリーンアップ専用、通常業務では呼び出し不要）

デフォルト設定：

| 項目 | デフォルト値 |
|---|---|
| `maxSaved` | 10 |
| `CallStackSize` | 4096 |
| `RegistrySize` | 4096 |
| `SkipOpenLibs` | true（各 VM は `Init` 時に個別に `OpenLibs` を呼び出し、プール汚染を回避） |

`newStatePoolWithOptions(Lua.Options{...})` でカスタマイズ可能。

## テスト

```bash
cd lua
go test -v ./...
```

カバレッジ：

- 基本実行 + グローバル変数読み書き + ホスト関数注入
- 並行 `CallFunction` + `GetGlobal` ストレステスト（50 goroutine × 200 ループ）
- 並行 `Init` / `Close` ストレステスト（40 goroutine × 200 操作）
- `Source` 注入 + `Load` / `LoadMulti` / `ExecuteFromKey` / `ExecuteFromKeys`
- `FileSource` エンドツーエンド（`t.TempDir()` + 一時スクリプトファイル）
- `statePool` の `Borrow` / `Return` 基本機能
- Lua table ↔ Go struct マッピング（`gluamapper`）

## 関連ドキュメント

- ルートモジュール README：[../README_JP.md](../README_JP.md)
- Engine インターフェース定義：[../engine.go](../engine.go)
- ScriptSource 実装：[../source.go](../source.go)
- エンジンプール：[../engine_pool.go](../engine_pool.go) / [../engine_pool_autogrow.go](../engine_pool_autogrow.go)
- gopher-lua ドキュメント：https://github.com/yuin/gopher-lua
- gopher-lua-libs ドキュメント：https://github.com/vadv/gopher-lua-libs
- gluacrypto ドキュメント：https://github.com/tengattack/gluacrypto
