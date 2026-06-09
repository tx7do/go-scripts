# CEL Engine · Expression Engine

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-CELType-blue)](../types.go)

**Google Common Expression Language engine based on [cel-go](https://github.com/google/cel-go)**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The CEL engine uses Google's [cel-go](https://github.com/google/cel-go) library to compile and evaluate CEL (Common Expression Language) expressions. CEL is a non-Turing-complete expression language designed for simplicity, safety, and fast evaluation. Each "script" evaluates a single expression with compile-time type checking.

### Engine Characteristics

| Feature | Description |
| --- | --- |
| Library | `github.com/google/cel-go` v0.22.1 |
| Language | CEL spec (non-Turing-complete expression language) |
| Type Inference | Automatic CEL type inference from Go values via reflection |
| Env Rebuild | CEL environment rebuilt automatically when globals/functions are registered |
| Thread Safety | Dual-lock pattern (`RWMutex` + `execMutex`) |
| Hot Reload | Supports `StartWatch` / `StopWatch` |
| Type Constant | `scriptEngine.CELType` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/cel
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
    _ "github.com/tx7do/go-scripts/cel" // Register the CEL engine
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.CELType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // Inject variables
    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterGlobal("age", 25)

    // Register custom function
    _ = eng.RegisterFunction("isAdult", func(age int) bool {
        return age >= 18
    })

    // Evaluate expression
    result, _ := eng.ExecuteString(ctx, "check", `isAdult(age) && name == "world"`)
    fmt.Println(result) // true
}
```

---

## API Reference

| Method | Description |
| --- | --- |
| `Init(ctx)` | Creates the CEL environment |
| `Close()` | Releases engine resources |
| `LoadString(ctx, name, code)` | Compiles CEL expression and enqueues |
| `Execute(ctx)` | Sequentially evaluates all queued expressions, returns last result |
| `ExecuteString(ctx, name, code)` | Compiles and immediately evaluates inline CEL expression |
| `RegisterGlobal(name, value)` | Registers global variable with auto-inferred CEL type |
| `RegisterFunction(name, fn)` | Registers Go function with types inferred via reflection |
| `CallFunction(ctx, name, args...)` | Imperatively calls a registered host function |
| `RegisterModule(name, module)` | Registers module (keys mapped as `modulename_keyname`) |
| `StartWatch(ctx, key)` | Starts expression hot-reload watching |
| `StopWatch(key)` | Stops hot-reload watching |

---

## Testing

```bash
cd cel && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [cel-go Official Documentation](https://github.com/google/cel-go)
- [CEL Language Specification](https://github.com/google/cel-spec)

## License

[MIT License](../LICENSE)
