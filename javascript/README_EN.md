# JavaScript Script Engine

A Go-embedded JavaScript scripting engine implementation based on [goja](https://github.com/dop251/goja), provided as a sub-module of [`go-scripts`](../).

## Design Highlights

- **Unified Interface**: Implements the [`script_engine.Engine`](../engine.go) interface, working seamlessly with root module components such as [`Manager`](../manager.go), [`EnginePool`](../engine_pool.go), and [`AutoGrowEnginePool`](../engine_pool_autogrow.go).
- **Auto Factory Registration**: Registered to the [`script_engine`](../factory.go) global factory table via `init()`. Just `import _ "github.com/tx7do/go-scripts/javascript"` to enable.
- **Concurrency Safe**: Internal `sync.RWMutex` + `execMu` double-lock guards runtime / programs / source; `Execute*` family supports `ctx.Done()` interruption via `goja.Runtime.Interrupt`.
- **Script Source Decoupling**: Script sources are injected through the `Source` interface (`FileSource` / `MemSource` / `MultiSource` / custom extensions); the engine no longer couples with any IO details.
- **Node.js Compatibility Layer**: Built-in `require` / `console` / `process` goja_nodejs modules are enabled; CommonJS-style module loading is available out of the box.
- **ES6 Subset Support**: goja supports all of ES5 + a subset of ES6 (`let` / `const` / template strings / arrow functions / destructuring, etc.).

## Dependencies

- Go 1.24+
- [`github.com/dop251/goja`](https://github.com/dop251/goja)
- [`github.com/dop251/goja_nodejs`](https://github.com/dop251/goja_nodejs) (provides `require` / `console` / `process`)
- [`github.com/tx7do/go-scripts`](../) (root module)

## Quick Start

### 1. Import

```go
import (
    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/javascript" // Register JavaScript factory
)
```

Once `init()` fires, all subsequent `scriptEngine.NewScriptEngine(scriptEngine.JavaScriptType, ...)` calls return an engine instance from this module.

### 2. Single Instance Usage

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

// Inject variable (host -> JS)
_ = eng.RegisterGlobal("answer", 42)

// Inject function (host -> JS)
_ = eng.RegisterFunction("sayHello", func(name string) {
    fmt.Println("Hello,", name)
})

// Execute inline script
result, err := eng.ExecuteString(ctx, "demo.js", `
    sayHello("world");
    answer + 100;
`)
// result = 142
```

### 3. Engine Pool (Recommended for Production)

```go
// Fixed-size pool
pool, err := scriptEngine.NewEnginePool(8, scriptEngine.JavaScriptType)

// Or: auto-grow pool (initial 2, upper bound 16)
pool, err := scriptEngine.NewAutoGrowEnginePool(2, 16, scriptEngine.JavaScriptType)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

ctx := context.Background()
_, _ = pool.ExecuteString(ctx, "init.js", `globalThis.appName = "demo";`)
```

### 4. With ScriptSource (Unified Script Source)

```go
// Local files + mtime hot-reload detection
src := scriptEngine.NewFileSource()
eng.SetSource(src)

// Load and execute from Source
result, err := eng.ExecuteFromKey(ctx, "/path/to/script.js")

// Also available via engine pool wrapper
pool.SetSource(src)
results, err := pool.ExecuteFromKeys(ctx, []string{"a.js", "b.js"})
```

You can also use `MemSource` (pure in-memory, zero IO) or `MultiSource` (multi-source aggregation / fallback).

## Core API

The `Engine` interface provides the following methods (excerpt; full definition at [`engine.go`](../engine.go)):

### Lifecycle

| Method | Description |
|---|---|
| `Init(ctx)` | Initialize the runtime; must be called before any Load*/Execute* |
| `Close()` | Release the runtime; you need to re-Init before reusing |
| `IsInitialized()` | Query initialization state |

### ScriptSource

| Method | Description |
|---|---|
| `SetSource(source)` | Bind a script source (FileSource / S3 / Mem / Multi / ...); pass `nil` to clear |
| `GetSource()` | Return the currently bound Source (nil if unbound) |

### Script Loading

| Method | Description |
|---|---|
| `Load(ctx, key)` | Load a single script from the bound Source (`key` is path / object key / script id) |
| `LoadMulti(ctx, keys)` | Batch load; aborts on first error |
| `LoadString(ctx, name, code)` | Compile inline script directly, **does not go through Source**. `name` is for diagnostics (appears in stack trace) |

### Script Execution

| Method | Description |
|---|---|
| `Execute(ctx)` | Execute all `Load*`-ed scripts, returning results in order |
| `ExecuteFromKey(ctx, key)` | Load from Source + immediate execute, one stop |
| `ExecuteFromKeys(ctx, keys)` | Multi-key version; result order matches `keys` |
| `ExecuteString(ctx, name, code)` | Compile and immediately execute inline script, **does not go through Source** |

### Global Variables / Functions / Modules

| Method | Description |
|---|---|
| `RegisterGlobal(name, value)` | Register or override a JS global (any Go value, auto-converted by goja) |
| `GetGlobal(name)` | Read a JS global; returns error when undefined |
| `RegisterFunction(name, fn)` | Register a host function; script can call by name directly |
| `CallFunction(ctx, name, args...)` | Call a script function by name; supports `ctx` timeout / interrupt |
| `RegisterModule(name, module)` | Register a module. `module` can be `map[string]any` (exports multiple members) or a single value |

### Error Handling

| Method | Description |
|---|---|
| `GetLastError()` | Get the most recent error |
| `ClearError()` | Clear the recent error state |

## Host ↔ Script Interop

### Variables

Three access modes are supported:

- **Write-only**: Inject host variables into the script via `RegisterGlobal`.
- **Read-only**: Read variables defined in the script via `GetGlobal`.
- **Bidirectional read/write**: Pass a pointer / struct reference; script-side modifications to its fields are reflected back to the host (goja auto-bridging).

### Functions

- **Script calling host**: After `RegisterFunction`, the script calls by name directly.
- **Host calling script**: First `ExecuteString` to define the function (e.g., `function add(a,b){return a+b}`), then `CallFunction(ctx, "add", 1, 2)`.

### Modules

`RegisterModule(name, module)` encapsulates a set of related variables / functions under a namespace, avoiding polluting the global stack:

```go
_ = eng.RegisterModule("mathutil", map[string]any{
    "square": func(x float64) float64 { return x * x },
    "pi":     3.14159,
})
```

```javascript
// Script side
mathutil.square(mathutil.pi * 2)
```

### CommonJS Modules (`require`)

This module enables `goja_nodejs`'s `require` / `console` / `process` at initialization, so standard Node.js-style modules work in scripts:

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

`require` resolves paths relative to the current working directory or script directory by default. See [goja_nodejs require](https://github.com/dop251/goja_nodejs).

## Complete Example

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

    // 1. Inject struct (script can modify its fields)
    u := &User{Name: "Tim"}
    _ = pool.RegisterGlobal("u", u)

    // 2. Inject host function
    _ = pool.RegisterFunction("sayHello", func(name string) {
        fmt.Println("Hello,", name)
    })

    // 3. Inject module
    _ = pool.RegisterModule("config", map[string]any{
        "env":  "prod",
        "port": 8080,
    })

    // 4. Execute inline script
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

## Testing

```bash
cd javascript
go test -v ./...
```

Coverage:

- Basic execution + global variable read/write + host function injection
- Concurrent `ExecuteString` / `CallFunction` stress test
- Concurrent `Init` / `Close` / `Execute` stress test
- `Source` injection + `Load` / `LoadMulti` / `ExecuteFromKey` / `ExecuteFromKeys`
- `FileSource` end-to-end (`t.TempDir()` + temp script file)

## Related Documentation

- Root module README: [../README_EN.md](../README_EN.md)
- Engine interface definition: [../engine.go](../engine.go)
- ScriptSource implementation: [../source.go](../source.go)
- Engine Pool: [../engine_pool.go](../engine_pool.go) / [../engine_pool_autogrow.go](../engine_pool_autogrow.go)
- goja docs: https://github.com/dop251/goja
