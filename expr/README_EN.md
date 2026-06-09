# Expr Engine · Expression Engine

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-ExprType-blue)](../types.go)

**Lightweight expression engine based on [expr-lang/expr](https://github.com/expr-lang/expr)**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Expr engine uses the [expr-lang/expr](https://github.com/expr-lang/expr) library to provide fast, type-safe expression evaluation. It supports variables, functions, and a rich set of built-in operators, making it ideal for business expressions, template engines, and data filtering.

### Engine Characteristics

| Feature | Description |
| --- | --- |
| Library | `github.com/expr-lang/expr` v1.17.0 |
| Language | Expr DSL |
| Type System | Duck typing with compile-time type checking |
| Env Snapshot | Creates environment snapshot on each compile/eval for consistency |
| Thread Safety | Dual-lock pattern (`RWMutex` + `execMutex`) |
| Hot Reload | Supports `StartWatch` / `StopWatch` |
| Type Constant | `scriptEngine.ExprType` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/expr
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
    _ "github.com/tx7do/go-scripts/expr" // Register the Expr engine
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.ExprType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // Inject variables
    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterGlobal("items", []int{1, 2, 3, 4, 5})

    // Register function
    _ = eng.RegisterFunction("double", func(x int) int { return x * 2 })

    // Evaluate expression
    result, _ := eng.ExecuteString(ctx, "filter", `items | filter(# > 2) | map(double(#))`)
    fmt.Println(result) // [6, 8, 10]
}
```

---

## API Reference

| Method | Description |
| --- | --- |
| `Init(ctx)` | Initializes the engine environment |
| `Close()` | Releases engine resources |
| `LoadString(ctx, name, code)` | Compiles Expr expression and enqueues |
| `Execute(ctx)` | Sequentially evaluates all queued expressions, returns last result |
| `ExecuteString(ctx, name, code)` | Compiles and immediately evaluates inline Expr expression |
| `RegisterGlobal(name, value)` | Registers a global variable in the expression environment |
| `RegisterFunction(name, fn)` | Registers a Go function in the expression environment |
| `CallFunction(ctx, name, args...)` | Imperatively calls a registered host function |
| `RegisterModule(name, module)` | Registers a namespace module |
| `StartWatch(ctx, key)` | Starts expression hot-reload watching |
| `StopWatch(key)` | Stops hot-reload watching |

---

## Testing

```bash
cd expr && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [Expr Official Documentation](https://github.com/expr-lang/expr)
- [Expr Language Reference](https://expr-lang.org/docs/language-definition)

## License

[MIT License](../LICENSE)
