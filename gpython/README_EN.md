# GPython Engine · Python Script Engine

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-GPythonType-blue)](../types.go)

**Pure-Go Python 3.4 subset script engine based on [gpython](https://github.com/go-python/gpython)**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The GPython engine uses the [go-python/gpython](https://github.com/go-python/gpython) library to provide Python 3.4 subset scripting capabilities for Go applications. Zero CPython dependency, pure Go implementation, cross-platform compilation without barriers.

### Engine Characteristics

| Feature | Description |
| --- | --- |
| Library | `github.com/go-python/gpython` v0.2.0 |
| Language Version | Python 3.4 subset |
| Standard Library | Built-in `sys`, `time`, `math`, and other native modules |
| Thread Safety | Dual-lock pattern (`RWMutex` + `execMutex`) |
| Hot Reload | Supports `StartWatch` / `StopWatch` |
| Type Constant | `scriptEngine.GPythonType` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/gpython
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
    _ "github.com/tx7do/go-scripts/gpython" // Register the GPython engine
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.GPythonType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    if err := eng.Init(ctx); err != nil {
        log.Fatal(err)
    }

    // Inject host variables
    _ = eng.RegisterGlobal("name", "world")

    // Register host functions
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // Execute Python script
    result, _ := eng.ExecuteString(ctx, "hello.py", `greet(name)`)
    fmt.Println(result)
}
```

---

## API Reference

| Method | Description |
| --- | --- |
| `Init(ctx)` | Creates gpython interpreter context and main module |
| `Close()` | Closes interpreter, releases resources |
| `LoadString(ctx, name, code)` | Compiles Python source and enqueues for execution |
| `Execute(ctx)` | Sequentially executes all queued scripts |
| `ExecuteString(ctx, name, code)` | Compiles and immediately executes inline Python code |
| `RegisterGlobal(name, value)` | Injects Go value as Python global variable |
| `RegisterFunction(name, fn)` | Registers Go function as Python callable |
| `CallFunction(ctx, name, args...)` | Calls a Python function |
| `RegisterModule(name, module)` | Registers a module (keys mapped as `modulename_keyname` globals) |
| `StartWatch(ctx, key)` | Starts script hot-reload watching |
| `StopWatch(key)` | Stops hot-reload watching |

---

## Testing

```bash
cd gpython && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/gpython)

## License

[MIT License](../LICENSE)
