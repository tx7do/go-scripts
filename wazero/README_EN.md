# Wazero Engine · WebAssembly Script Engine

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-WazeroType-blue)](../types.go)

**Zero-CGO WebAssembly runtime engine based on [wazero](https://github.com/tetratelabs/wazero)**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Wazero engine uses the [tetratelabs/wazero](https://github.com/tetratelabs/wazero) library to provide WebAssembly (WASM) module loading and execution capabilities for Go applications. WASM is a binary format, so the `code` parameter of `LoadString` / `ExecuteString` is raw WASM bytes.

### Engine Characteristics

| Feature | Description |
| --- | --- |
| Library | `github.com/tetratelabs/wazero` v1.9.0 |
| Format | WASM 1.0 binary |
| Host Functions | Exposed via `host` import module; WASM modules can `import "host"` |
| Function Calls | `CallFunction` invokes exported WASM functions with `uint64` args |
| Thread Safety | Dual-lock pattern (`RWMutex` + `execMutex`) |
| Hot Reload | Supports `StartWatch` / `StopWatch` |
| Type Constant | `scriptEngine.WazeroType` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/wazero
```

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/wazero" // Register the Wazero engine
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.WazeroType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // Load WASM module (raw bytes)
    wasmBytes, _ := os.ReadFile("add.wasm")
    _ = eng.LoadString(ctx, "add.wasm", string(wasmBytes))

    // Instantiate and run (calls _start)
    eng.Execute(ctx)

    // Call exported WASM function
    result, _ := eng.CallFunction(ctx, "add", uint64(3), uint64(4))
    fmt.Println(result) // 7
}
```

---

## API Reference

| Method | Description |
| --- | --- |
| `Init(ctx)` | Creates the wazero runtime |
| `Close()` | Closes runtime and all compiled modules |
| `LoadString(ctx, name, code)` | Compiles WASM bytes and enqueues |
| `Execute(ctx)` | Instantiates the last module and calls `_start` |
| `ExecuteString(ctx, name, code)` | Compiles and immediately instantiates WASM bytes |
| `RegisterFunction(name, fn)` | Registers host function in the `host` import module |
| `CallFunction(ctx, name, args...)` | Calls an exported WASM function (args as `uint64`) |
| `RegisterModule(name, module)` | Registers a named host module |
| `GetGlobal(name)` | Reads a WASM exported global (returns `uint64`) |
| `StartWatch(ctx, key)` | Starts WASM module hot-reload watching |
| `StopWatch(key)` | Stops hot-reload watching |

> **Note**: Host functions registered via `RegisterFunction` must have parameters compatible with WASM calling conventions (`context.Context`, `api.Module`, or numeric types `uint32/uint64/int32/int64/float32/float64`).

---

## Testing

```bash
cd wazero && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [wazero Official Documentation](https://github.com/tetratelabs/wazero)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/wazero)

## License

[MIT License](../LICENSE)
