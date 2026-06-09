# CEL Engine · 表达式引擎

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-CELType-blue)](../types.go)

**基于 [cel-go](https://github.com/google/cel-go) 的 Google 通用表达式语言引擎**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

CEL 引擎使用 Google 的 [cel-go](https://github.com/google/cel-go) 库，提供 CEL (Common Expression Language) 表达式的编译与求值。CEL 是非图灵完备的表达式语言，专为简洁、安全和快速求值设计。每个"脚本"求值一个独立表达式，支持编译时类型检查。

### 引擎特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/google/cel-go` v0.22.1 |
| 语言 | CEL 规范（非图灵完备表达式语言） |
| 类型推断 | 通过反射自动推断 Go 值的 CEL 类型 |
| 环境重建 | 注册全局变量/函数时自动重建 CEL 环境 |
| 线程安全 | 双锁模式（`RWMutex` + `execMutex`） |
| 热更新 | 支持 `StartWatch` / `StopWatch` |
| 类型常量 | `scriptEngine.CELType` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/cel
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
    _ "github.com/tx7do/go-scripts/cel" // 注册 CEL 引擎
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.CELType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // 注入变量
    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterGlobal("age", 25)

    // 注册自定义函数
    _ = eng.RegisterFunction("isAdult", func(age int) bool {
        return age >= 18
    })

    // 求值表达式
    result, _ := eng.ExecuteString(ctx, "check", `isAdult(age) && name == "world"`)
    fmt.Println(result) // true
}
```

---

## API 参考

| 方法 | 说明 |
| --- | --- |
| `Init(ctx)` | 创建 CEL 环境 |
| `Close()` | 释放引擎资源 |
| `LoadString(ctx, name, code)` | 编译 CEL 表达式并加入队列 |
| `Execute(ctx)` | 顺序求值队列中所有表达式，返回最后一个结果 |
| `ExecuteString(ctx, name, code)` | 编译并立即求值内联 CEL 表达式 |
| `RegisterGlobal(name, value)` | 注册全局变量，自动推断 CEL 类型 |
| `RegisterFunction(name, fn)` | 注册 Go 函数，参数/返回值类型通过反射推断 |
| `CallFunction(ctx, name, args...)` | 命令式调用已注册的宿主函数 |
| `RegisterModule(name, module)` | 注册模块（键映射为 `模块名_键名` 全局变量） |
| `StartWatch(ctx, key)` | 启动表达式热更新监听 |
| `StopWatch(key)` | 停止热更新监听 |

---

## 测试

```bash
cd cel && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [cel-go 官方文档](https://github.com/google/cel-go)
- [CEL 语言规范](https://github.com/google/cel-spec)

## License

[MIT License](../LICENSE)
