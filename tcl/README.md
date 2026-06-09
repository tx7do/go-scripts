# TCL Engine · TCL 脚本引擎

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-TclType-blue)](../types.go)

**基于 [modernc.org/tcl](https://pkg.go.dev/modernc.org/tcl) 的 100% 兼容 Tcl 引擎**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

TCL 引擎使用 [modernc.org/tcl](https://pkg.go.dev/modernc.org/tcl) 库 —— 一个无 CGO 的 Tcl 移植版本。为 Go 应用提供完整的 Tool Command Language 脚本执行能力，适用于传统系统兼容和网络设备脚本等场景。

### 引擎特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `modernc.org/tcl` v1.15.3 |
| 语言版本 | TCL 8.6（100% 兼容） |
| CGO 依赖 | 无（纯 Go 移植） |
| 自动挂载 | 启动时自动挂载 TCL 库 VFS |
| 线程安全 | 双锁模式（`RWMutex` + `execMutex`） |
| 热更新 | 支持 `StartWatch` / `StopWatch` |
| 类型常量 | `scriptEngine.TclType` |

> **平台注意**：在 Windows 上，`modernc.org/tcl` 的 `Close()` 可能出现 notifier 死锁。引擎在 Go 层面正确清理状态，但底层解释器的销毁依赖进程退出。

---

## 安装

```bash
go get github.com/tx7do/go-scripts/tcl
```

---

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "log"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/tcl" // 注册 TCL 引擎
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.TclType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // 注入变量
    _ = eng.RegisterGlobal("name", "world")

    // 注册命令
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // 执行 TCL 脚本
    result, _ := eng.ExecuteString(ctx, "hello.tcl", `greet $name`)
    fmt.Println(result) // Hello, world!
}
```

---

## API 参考

| 方法 | 说明 |
| --- | --- |
| `Init(ctx)` | 创建 TCL 解释器，挂载库 VFS |
| `Close()` | 清理引擎状态（Go 层面） |
| `LoadString(ctx, name, code)` | 将 TCL 源码加入执行队列 |
| `Execute(ctx)` | 顺序执行队列中所有脚本，共享解释器 |
| `ExecuteString(ctx, name, code)` | 立即执行内联 TCL 代码 |
| `RegisterGlobal(name, value)` | 通过 `set` 命令设置 TCL 变量 |
| `RegisterFunction(name, fn)` | 将 Go 函数注册为 TCL 命令 |
| `CallFunction(ctx, name, args...)` | 调用 TCL 过程或已注册的命令 |
| `RegisterModule(name, module)` | 注册模块（键映射为 `模块名_键名` 变量） |
| `StartWatch(ctx, key)` | 启动脚本热更新监听 |
| `StopWatch(key)` | 停止热更新监听 |

---

## 测试

```bash
cd tcl && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [modernc.org/tcl 文档](https://pkg.go.dev/modernc.org/tcl)
- [TCL 官方网站](https://tcl.tk)

## License

[MIT License](../LICENSE)
