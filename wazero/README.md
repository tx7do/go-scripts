# Wazero Engine · WebAssembly 脚本引擎

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-WazeroType-blue)](../types.go)

**基于 [wazero](https://github.com/tetratelabs/wazero) 的零 CGO WebAssembly 运行时引擎**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Wazero 引擎使用 [tetratelabs/wazero](https://github.com/tetratelabs/wazero) 库，为 Go 应用提供 WebAssembly (WASM) 模块的加载与执行能力。WASM 是二进制格式，因此 `LoadString` / `ExecuteString` 的 `code` 参数是原始 WASM 字节。

### 引擎特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/tetratelabs/wazero` v1.9.0 |
| 格式 | WASM 1.0 二进制 |
| 宿主函数 | 通过 `host` 导入模块暴露宿主函数，WASM 模块可 `import "host"` |
| 函数调用 | `CallFunction` 使用 `uint64` 参数调用导出的 WASM 函数 |
| 线程安全 | 双锁模式（`RWMutex` + `execMutex`） |
| 热更新 | 支持 `StartWatch` / `StopWatch` |
| 类型常量 | `scriptEngine.WazeroType` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/wazero
```

---

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/wazero" // 注册 Wazero 引擎
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.WazeroType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // 加载 WASM 模块（原始字节）
    wasmBytes, _ := os.ReadFile("add.wasm")
    _ = eng.LoadString(ctx, "add.wasm", string(wasmBytes))

    // 实例化并运行（调用 _start）
    eng.Execute(ctx)

    // 调用导出的 WASM 函数
    result, _ := eng.CallFunction(ctx, "add", uint64(3), uint64(4))
    fmt.Println(result) // 7
}
```

---

## API 参考

| 方法 | 说明 |
| --- | --- |
| `Init(ctx)` | 创建 wazero 运行时 |
| `Close()` | 关闭运行时及所有已编译模块 |
| `LoadString(ctx, name, code)` | 编译 WASM 字节码并加入队列 |
| `Execute(ctx)` | 实例化最后一个模块并调用 `_start` |
| `ExecuteString(ctx, name, code)` | 编译并立即实例化 WASM 字节码 |
| `RegisterFunction(name, fn)` | 注册宿主函数到 `host` 导入模块 |
| `CallFunction(ctx, name, args...)` | 调用导出的 WASM 函数（参数为 `uint64`） |
| `RegisterModule(name, module)` | 注册命名宿主模块 |
| `GetGlobal(name)` | 读取 WASM 导出的全局变量（返回 `uint64`） |
| `StartWatch(ctx, key)` | 启动 WASM 模块热更新监听 |
| `StopWatch(key)` | 停止热更新监听 |

> **注意**：`RegisterFunction` 注册的宿主函数参数必须兼容 WASM 调用约定（`context.Context`、`api.Module` 或数值类型 `uint32/uint64/int32/int64/float32/float64`）。

---

## 测试

```bash
cd wazero && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [wazero 官方文档](https://github.com/tetratelabs/wazero)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/wazero)

## License

[MIT License](../LICENSE)
