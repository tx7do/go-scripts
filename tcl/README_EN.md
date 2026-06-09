# TCL Engine · TCL Script Engine

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-TclType-blue)](../types.go)

**100% compatible Tcl engine based on [modernc.org/tcl](https://pkg.go.dev/modernc.org/tcl)**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The TCL engine uses the [modernc.org/tcl](https://pkg.go.dev/modernc.org/tcl) library — a CGO-free port of Tcl. It provides full Tool Command Language scripting capabilities for Go applications, suitable for legacy system integration and network device scripting.

### Engine Characteristics

| Feature | Description |
| --- | --- |
| Library | `modernc.org/tcl` v1.15.3 |
| Language Version | TCL 8.6 (100% compatible) |
| CGO Dependency | None (pure Go port) |
| Auto-mount | Automatically mounts TCL library VFS on startup |
| Thread Safety | Dual-lock pattern (`RWMutex` + `execMutex`) |
| Hot Reload | Supports `StartWatch` / `StopWatch` |
| Type Constant | `scriptEngine.TclType` |

> **Platform Note**: On Windows, `modernc.org/tcl`'s `Close()` may encounter notifier deadlock. The engine correctly cleans up Go-side state, but underlying interpreter destruction relies on process exit.

---

## Installation

```bash
go get github.com/tx7do/go-scripts/tcl
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
    _ "github.com/tx7do/go-scripts/tcl" // Register the TCL engine
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.TclType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // Inject variables
    _ = eng.RegisterGlobal("name", "world")

    // Register command
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // Execute TCL script
    result, _ := eng.ExecuteString(ctx, "hello.tcl", `greet $name`)
    fmt.Println(result) // Hello, world!
}
```

---

## API Reference

| Method | Description |
| --- | --- |
| `Init(ctx)` | Creates TCL interpreter, mounts library VFS |
| `Close()` | Cleans up engine state (Go-side) |
| `LoadString(ctx, name, code)` | Enqueues TCL source code |
| `Execute(ctx)` | Sequentially executes all queued scripts, sharing interpreter |
| `ExecuteString(ctx, name, code)` | Immediately executes inline TCL code |
| `RegisterGlobal(name, value)` | Sets TCL variable via `set` command |
| `RegisterFunction(name, fn)` | Registers Go function as TCL command |
| `CallFunction(ctx, name, args...)` | Calls a TCL procedure or registered command |
| `RegisterModule(name, module)` | Registers a module (keys mapped as `modulename_keyname`) |
| `StartWatch(ctx, key)` | Starts script hot-reload watching |
| `StopWatch(key)` | Stops hot-reload watching |

---

## Testing

```bash
cd tcl && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [modernc.org/tcl Documentation](https://pkg.go.dev/modernc.org/tcl)
- [TCL Official Website](https://tcl.tk)

## License

[MIT License](../LICENSE)
