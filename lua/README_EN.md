# Lua Script Engine

A Go-embedded Lua scripting engine implementation based on [GopherLua](https://github.com/yuin/gopher-lua), provided as a sub-module of [`go-scripts`](../).

GopherLua is a pure-Go implementation of the Lua 5.1 virtual machine, with a style close to the C Lua C API, decent performance, and zero CGO dependencies.

## Design Highlights

- **Unified Interface**: Implements the [`script_engine.Engine`](../engine.go) interface, working seamlessly with root module components such as [`Manager`](../manager.go), [`EnginePool`](../engine_pool.go), and [`AutoGrowEnginePool`](../engine_pool_autogrow.go).
- **Auto Factory Registration**: Registered to the [`script_engine`](../factory.go) global factory table via `init()`. Just `import _ "github.com/tx7do/go-scripts/lua"` to enable.
- **Concurrency Safe**: Internal `sync.RWMutex` guards VM / source / initialized; `Execute*` / `CallFunction` support cancellation and timeout via channel + `ctx.Done()`.
- **LState Reuse Pool**: This module ships with a [`statePool`](state_pool.go) that lends out LStates during engine `Init` and returns them on `Close`, with a default upper bound of 10 LState instances to avoid rebuilding the VM on every run.
- **Script Source Decoupling**: Script sources are injected through the `Source` interface (`FileSource` / `MemSource` / `MultiSource` / custom extensions); the engine no longer couples with any IO details.
- **Standard Lua Ecosystem**: `Init` automatically enables the Lua standard library + [`gopher-lua-libs`](https://github.com/vadv/gopher-lua-libs) (json / http / regexp / db / time / ...) + [`gluacrypto`](https://github.com/tengattack/gluacrypto) (crypto / hash) + `GetLuaPath` helper.

## gopher-lua Limitations (Important)

| Limitation | Behavior | Workaround |
|---|---|---|
| **Single Compilation Unit** | LState retains only one compiled `LFunction` at a time; consecutive `Load` / `LoadString` calls overwrite each other | For multiple scripts, use `ExecuteFromKeys` or pair `Load` + `Execute` manually |
| **5.1 Subset** | No Lua 5.2+ `goto` / bit32 / 5.3 integer types | Write scripts in Lua 5.1 syntax |
| **No JIT** | Slower than LuaJIT, slightly slower than C Lua | For hot paths, consider pre-compilation or sinking logic to Go |
| **`LoadString` / `DoString` have no name parameter** | The `name` in `LoadString(ctx, name, code)` / `ExecuteString(ctx, name, code)` is only for interface compatibility and **is ignored** | Script name won't appear in stack traces |

## Dependencies

- Go 1.24+
- [`github.com/yuin/gopher-lua`](https://github.com/yuin/gopher-lua) — Lua 5.1 VM
- [`layeh.com/gopher-luar`](https://github.com/layeh/gopher-luar) — Go ↔ Lua bidirectional value bridging
- [`github.com/yuin/gluamapper`](https://github.com/yuin/gluamapper) — Lua table ↔ Go struct mapping
- [`github.com/vadv/gopher-lua-libs`](https://github.com/vadv/gopher-lua-libs) — Common library extensions (json/http/regexp/db/...)
- [`github.com/tengattack/gluacrypto`](https://github.com/tengattack/gluacrypto) — Crypto / hash extensions
- [`github.com/tx7do/go-scripts`](../) — Root module

## Quick Start

### 1. Import

```go
import (
    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua" // Register Lua factory
)
```

Once `init()` fires, all subsequent `scriptEngine.NewScriptEngine(scriptEngine.LuaType, ...)` calls return an engine instance from this module.

### 2. Single Instance Usage

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

// Inject variable (host -> Lua)
_ = eng.RegisterGlobal("answer", 42)

// Inject host function (must be Lua.LGFunction)
_ = eng.RegisterFunction("say_hello", func(L *lua.LState) int {
    name := L.CheckString(1)
    fmt.Println("Hello,", name)
    return 0 // number of return values
})

// Execute inline script
_, err = eng.ExecuteString(ctx, "demo.lua", `
    say_hello("world")
    print(answer + 100)
`)
```

### 3. Engine Pool (Recommended for Production)

```go
// Fixed-size pool
pool, err := scriptEngine.NewEnginePool(8, scriptEngine.LuaType)

// Or: auto-grow pool (initial 2, upper bound 16)
pool, err := scriptEngine.NewAutoGrowEnginePool(2, 16, scriptEngine.LuaType)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

ctx := context.Background()
_, _ = pool.ExecuteString(ctx, "init.lua", `app_name = "demo"`)
```

### 4. With ScriptSource (Unified Script Source)

```go
// Local files + mtime hot-reload detection
src := scriptEngine.NewFileSource()
eng.SetSource(src)

// Load and execute from Source
_, err := eng.ExecuteFromKey(ctx, "/path/to/script.lua")

// Also available via engine pool wrapper
pool.SetSource(src)
results, err := pool.ExecuteFromKeys(ctx, []string{"a.lua", "b.lua"})
```

You can also use `MemSource` (pure in-memory, zero IO) or `MultiSource` (multi-source aggregation / fallback).

## Core API

The `Engine` interface provides the following methods (excerpt; full definition at [`engine.go`](../engine.go)):

### Lifecycle

| Method | Description |
|---|---|
| `Init(ctx)` | Borrows an LState from `statePool` and opens standard libraries; must be called before any Load*/Execute* |
| `Close()` | Returns the LState to the pool; you need to re-Init before reusing |
| `IsInitialized()` | Query initialization state |

### ScriptSource

| Method | Description |
|---|---|
| `SetSource(source)` | Bind a script source (FileSource / S3 / Mem / Multi / ...); pass `nil` to clear |
| `GetSource()` | Return the currently bound Source (nil if unbound) |

### Script Loading

| Method | Description |
|---|---|
| `Load(ctx, key)` | Load a single script from the bound Source. **Note**: gopher-lua keeps only one compiled function at a time; consecutive Loads overwrite each other |
| `LoadMulti(ctx, keys)` | Batch load; aborts on first error. Same overwrite rule applies |
| `LoadString(ctx, name, code)` | Compile inline script directly, **does not go through Source**. `name` is ignored (gopher-lua doesn't support it) |

### Script Execution

| Method | Description |
|---|---|
| `Execute(ctx)` | Execute the function compiled by the last `Load*`; ctx cancellation triggers channel-based abort |
| `ExecuteFromKey(ctx, key)` | Load from Source + immediate execute, one stop |
| `ExecuteFromKeys(ctx, keys)` | Multi-key version; result order matches `keys` (recommended) |
| `ExecuteString(ctx, name, code)` | Compile and immediately execute inline script, **does not go through Source**. `name` is ignored |

### Global Variables / Functions / Modules

| Method | Description |
|---|---|
| `RegisterGlobal(name, value)` | Bridge a Go value into Lua global via `luar`; supports primitives, maps, struct pointers (**fields are bidirectionally read/write**) |
| `GetGlobal(name)` | Read a Lua global; auto-converted to `interface{}` (LNumber → int64/float64, LTable → map/slice) |
| `RegisterFunction(name, fn)` | Register a Lua function. `fn` **must be** of type `Lua.LGFunction`, otherwise returns error |
| `CallFunction(ctx, name, args...)` | Call a Lua function by name; args auto-convert to LValue, return values auto-convert to Go values |
| `RegisterModule(name, module)` | Register a Lua module. `module` **must be** of type `Lua.LGFunction` (module loader function) |

### Error Handling

| Method | Description |
|---|---|
| `GetLastError()` | Get the most recent error |
| `ClearError()` | Clear the recent error state |

## Host ↔ Script Interop

### Variables

Three access modes are supported:

- **Write-only**: `RegisterGlobal` injects host variables into the script (primitives / maps / slices).
- **Read-only**: `GetGlobal` reads variables defined in the script.
- **Bidirectional read/write**: Inject a **struct pointer** via `luar`; script-side modifications to its fields are reflected back to the host.

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

### Functions

**Lua function signature requirement**: This module's `RegisterFunction` / `RegisterModule` **only accepts** `Lua.LGFunction` (i.e., `func(*lua.LState) int`); other types will error out.

```go
import Lua "github.com/yuin/gopher-lua"

// host -> Lua
_ = eng.RegisterFunction("add", func(L *Lua.LState) int {
    a := L.CheckInt(1)
    b := L.CheckInt(2)
    L.Push(Lua.LNumber(a + b))
    return 1 // number of return values
})

// Lua -> host
_, _ = eng.ExecuteString(ctx, "", `result = add(10, 20)`)
v, _ := eng.GetGlobal("result") // int64(30)
```

Host calling a function defined in the script:

```go
_, _ = eng.ExecuteString(ctx, "", `
    function multiply(a, b) return a * b end
`)
v, _ := eng.CallFunction(ctx, "multiply", 3, 4) // int64(12)
```

### Modules

`RegisterModule` registers a Lua module loader function (of type `Lua.LGFunction`):

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
-- Script side
print(mathutil.pi)               -- 3.14159
print(mathutil.square(2))        -- 4
```

> Note: The `RegisterModule` above uses gopher-lua's built-in loader protocol (`L.NewFunction + L.Call`); the `module` table + `return module` style shown in `script/test_module.lua` is for **`require` loading scenarios**. The two are not mutually exclusive and can coexist.

### Lua Built-in Modules (Auto-enabled)

`virtualMachine.init()` automatically enables the following libraries:

| Library | Source | Purpose |
|---|---|---|
| `string` / `table` / `math` / `io` / `os` / `debug` / `package` | gopher-lua stdlib | Lua 5.1 built-in |
| `json` | gopher-lua-libs | JSON encoding/decoding |
| `http` | gopher-lua-libs | HTTP client |
| `regexp` | gopher-lua-libs | Regex matching (based on Go regexp) |
| `db` | gopher-lua-libs | MySQL / SQLite / PostgreSQL access |
| `time` | gopher-lua-libs | Time operations |
| `crypto` | gluacrypto | md5 / sha / hmac / aes / des / rsa etc. |
| `GetLuaPath()` | Custom | Returns the `script` subdirectory under the current executable's directory, for splicing `package.path` |

Standard `require` syntax works:

```lua
-- script/test_module.lua
module = {}
module.constant = "this is a constant"
function module.func3() print("hello") end
return module

-- script/main.lua
local m = require("test_module")
print(m.constant)
m.func3()
```

## Complete Example

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

    // 1. Inject struct (script can modify its fields)
    u := &User{Name: "Tim"}
    _ = pool.RegisterGlobal("u", u)

    // 2. Inject host function (must be Lua.LGFunction)
    _ = pool.RegisterFunction("say_hello", func(L *Lua.LState) int {
        name := L.CheckString(1)
        fmt.Println("Hello,", name)
        return 0
    })

    // 3. Execute inline script
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

## LState Pool

This module internally maintains a [`statePool`](state_pool.go):

- `Borrow()` — get an idle LState, or create one if none available
- `Return(L)` — return an LState; Close if over the limit
- `Shutdown()` — shut down the pool, reclaim all idle LStates (for global cleanup only; normal business need not call)

Default configuration:

| Item | Default |
|---|---|
| `maxSaved` | 10 |
| `CallStackSize` | 4096 |
| `RegistrySize` | 4096 |
| `SkipOpenLibs` | true (each VM calls `OpenLibs` during `Init` to avoid pool pollution) |

Customizable via `newStatePoolWithOptions(Lua.Options{...})`.

## Testing

```bash
cd lua
go test -v ./...
```

Coverage:

- Basic execution + global variable read/write + host function injection
- Concurrent `CallFunction` + `GetGlobal` stress test (50 goroutines × 200 loops)
- Concurrent `Init` / `Close` stress test (40 goroutines × 200 ops)
- `Source` injection + `Load` / `LoadMulti` / `ExecuteFromKey` / `ExecuteFromKeys`
- `FileSource` end-to-end (`t.TempDir()` + temp script file)
- `statePool` `Borrow` / `Return` basics
- Lua table ↔ Go struct mapping (`gluamapper`)

## Related Documentation

- Root module README: [../README_EN.md](../README_EN.md)
- Engine interface definition: [../engine.go](../engine.go)
- ScriptSource implementation: [../source.go](../source.go)
- Engine Pool: [../engine_pool.go](../engine_pool.go) / [../engine_pool_autogrow.go](../engine_pool_autogrow.go)
- gopher-lua docs: https://github.com/yuin/gopher-lua
- gopher-lua-libs docs: https://github.com/vadv/gopher-lua-libs
- gluacrypto docs: https://github.com/tengattack/gluacrypto
