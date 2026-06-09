# Starlark Engine · Python-Dialect Script Engine

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-StarlarkType-blue)](../types.go)

**Secure embedded scripting engine based on [google/starlark-go](https://github.com/google/starlark-go)**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Starlark engine uses Google's [starlark-go](https://github.com/google/starlark-go) library. Starlark is a dialect of Python designed for embedded scripting, featuring deterministic execution, thread safety, and sandboxed evaluation. It is widely used in the Bazel build system and similar tools.

### Engine Characteristics

| Feature | Description |
| --- | --- |
| Library | `go.starlark.net` |
| Language | Starlark (Python dialect) |
| Security | Deterministic execution, side-effect-free sandbox |
| Globals | `hostPredeclared` + `scriptGlobals` dual-layer namespace |
| Thread Safety | Dual-lock pattern (`RWMutex` + `execMutex`) |
| Hot Reload | Supports `StartWatch` / `StopWatch` |
| Type Constant | `scriptEngine.StarlarkType` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/starlark
```

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/starlark" // Register the Starlark engine
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.StarlarkType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // Inject variables
    _ = eng.RegisterGlobal("name", "world")

    // Register function
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // Execute Starlark script
    result, _ := eng.ExecuteString(ctx, "hello.star", `greet(name)`)
    fmt.Println(result) // Hello, world!
}
```

---

## API Reference

| Method | Description |
| --- | --- |
| `Init(ctx)` | Initializes the Starlark engine |
| `Close()` | Releases engine resources |
| `LoadString(ctx, name, code)` | Enqueues Starlark source code |
| `Execute(ctx)` | Sequentially executes all queued scripts, sharing globals |
| `ExecuteString(ctx, name, code)` | Compiles and immediately executes inline Starlark code |
| `RegisterGlobal(name, value)` | Registers a variable in `hostPredeclared` |
| `RegisterFunction(name, fn)` | Wraps Go function as Starlark `Builtin` |
| `CallFunction(ctx, name, args...)` | Calls a Starlark function or registered host function |
| `RegisterModule(name, module)` | Registers a module (keys mapped as `modulename_keyname`) |
| `StartWatch(ctx, key)` | Starts script hot-reload watching |
| `StopWatch(key)` | Stops hot-reload watching |

---

## Testing

```bash
cd starlark && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [Starlark Official Documentation](https://github.com/google/starlark-go)
- [Starlark Language Specification](https://docs.bazel.build/versions/main/skylark/language.html)

## License

[MIT License](../LICENSE)
