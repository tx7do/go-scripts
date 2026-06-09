# Starlark Engine · Python 方言脚本引擎

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-StarlarkType-blue)](../types.go)

**基于 [google/starlark-go](https://github.com/google/starlark-go) 的安全嵌入式脚本引擎**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Starlark 引擎使用 Google 的 [starlark-go](https://github.com/google/starlark-go) 库。Starlark 是 Python 的方言，专为嵌入式脚本设计，具有确定性、线程安全和沙箱化执行的特点。广泛应用于 Bazel 构建系统等场景。

### 引擎特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `go.starlark.net` |
| 语言 | Starlark（Python 方言） |
| 安全性 | 确定性执行、无副作用沙箱 |
| 全局变量 | `hostPredeclared` + `scriptGlobals` 双层命名空间 |
| 线程安全 | 双锁模式（`RWMutex` + `execMutex`） |
| 热更新 | 支持 `StartWatch` / `StopWatch` |
| 类型常量 | `scriptEngine.StarlarkType` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/starlark
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
    _ "github.com/tx7do/go-scripts/starlark" // 注册 Starlark 引擎
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.StarlarkType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // 注入变量
    _ = eng.RegisterGlobal("name", "world")

    // 注册函数
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // 执行 Starlark 脚本
    result, _ := eng.ExecuteString(ctx, "hello.star", `greet(name)`)
    fmt.Println(result) // Hello, world!
}
```

---

## API 参考

| 方法 | 说明 |
| --- | --- |
| `Init(ctx)` | 初始化 Starlark 引擎 |
| `Close()` | 释放引擎资源 |
| `LoadString(ctx, name, code)` | 将 Starlark 源码加入执行队列 |
| `Execute(ctx)` | 顺序执行队列中所有脚本，共享全局变量 |
| `ExecuteString(ctx, name, code)` | 编译并立即执行内联 Starlark 代码 |
| `RegisterGlobal(name, value)` | 在 `hostPredeclared` 中注册变量 |
| `RegisterFunction(name, fn)` | 将 Go 函数包装为 Starlark `Builtin` |
| `CallFunction(ctx, name, args...)` | 调用 Starlark 函数或已注册的宿主函数 |
| `RegisterModule(name, module)` | 注册模块（键映射为 `模块名_键名`） |
| `StartWatch(ctx, key)` | 启动脚本热更新监听 |
| `StopWatch(key)` | 停止热更新监听 |

---

## 测试

```bash
cd starlark && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [Starlark 官方文档](https://github.com/google/starlark-go)
- [Starlark 语言规范](https://docs.bazel.build/versions/main/skylark/language.html)

## License

[MIT License](../LICENSE)
