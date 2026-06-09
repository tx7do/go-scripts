# Yaegi Engine · Go Script Engine

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-YaegiType-blue)](../types.go)

**Native Go scripting engine based on [Traefik Yaegi](https://github.com/traefik/yaegi)**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

The Yaegi engine uses the [Traefik Yaegi](https://github.com/traefik/yaegi) library to provide native Go runtime scripting capabilities for Go applications. No compilation needed — interprets Go source code directly. Ideal for dynamic plugins, DevOps toolchains, and more.

### Engine Characteristics

| Feature | Description |
| --- | --- |
| Library | `github.com/traefik/yaegi` v0.16.1 |
| Language Version | Go 1.x |
| Host Interop | Globals and functions exposed via synthetic `host` package; scripts must `import "host"` |
| Thread Safety | Dual-lock pattern (`RWMutex` + `execMutex`) |
| Hot Reload | Supports `StartWatch` / `StopWatch` |
| Type Constant | `scriptEngine.YaegiType` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/yaegi
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
    _ "github.com/tx7do/go-scripts/yaegi" // Register the Yaegi engine
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.YaegiType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // Register host functions (scripts call via host.FuncName)
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // Execute Go script (note: requires import "host")
    result, _ := eng.ExecuteString(ctx, "hello.go", `
        import "host"
        host.greet("world")
    `)
    fmt.Println(result) // Hello, world!
}
```

> **Note**: In the Yaegi engine, variables and functions registered via `RegisterGlobal` / `RegisterFunction` are placed in a synthetic `host` package. Scripts must `import "host"` to access them.

---

## API Reference

| Method | Description |
| --- | --- |
| `Init(ctx)` | Creates the Yaegi interpreter instance |
| `Close()` | Closes interpreter, releases resources |
| `LoadString(ctx, name, code)` | Enqueues Go source code for execution |
| `Execute(ctx)` | Sequentially executes all queued scripts |
| `ExecuteString(ctx, name, code)` | Compiles and immediately executes inline Go code |
| `RegisterGlobal(name, value)` | Registers a global variable in the `host` package |
| `RegisterFunction(name, fn)` | Registers a Go function in the `host` package |
| `CallFunction(ctx, name, args...)` | Calls a registered function |
| `RegisterModule(name, module)` | Registers a custom package for script `import` |
| `StartWatch(ctx, key)` | Starts script hot-reload watching |
| `StopWatch(key)` | Stops hot-reload watching |

---

## Testing

```bash
cd yaegi && go test -v ./...
```

---

## Related Documentation

- [Back to Main Documentation](../README.md)
- [Yaegi Official Documentation](https://github.com/traefik/yaegi)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/yaegi)

## License

[MIT License](../LICENSE)
