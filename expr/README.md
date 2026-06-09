# Expr Engine · 表达式引擎

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-ExprType-blue)](../types.go)

**基于 [expr-lang/expr](https://github.com/expr-lang/expr) 的轻量级表达式引擎**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Expr 引擎使用 [expr-lang/expr](https://github.com/expr-lang/expr) 库，提供快速、类型安全的表达式求值。支持变量、函数和丰富的内置运算符，适用于业务表达式、模板引擎、数据筛选等场景。

### 引擎特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/expr-lang/expr` v1.17.0 |
| 语言 | Expr DSL |
| 类型系统 | Duck typing，编译时类型检查 |
| 环境快照 | 每次编译/执行时创建环境快照，保证一致性 |
| 线程安全 | 双锁模式（`RWMutex` + `execMutex`） |
| 热更新 | 支持 `StartWatch` / `StopWatch` |
| 类型常量 | `scriptEngine.ExprType` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/expr
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
    _ "github.com/tx7do/go-scripts/expr" // 注册 Expr 引擎
)

func main() {
    eng, _ := scriptEngine.NewScriptEngine(scriptEngine.ExprType)
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // 注入变量
    _ = eng.RegisterGlobal("name", "world")
    _ = eng.RegisterGlobal("items", []int{1, 2, 3, 4, 5})

    // 注册函数
    _ = eng.RegisterFunction("double", func(x int) int { return x * 2 })

    // 求值表达式
    result, _ := eng.ExecuteString(ctx, "filter", `items | filter(# > 2) | map(double(#))`)
    fmt.Println(result) // [6, 8, 10]
}
```

---

## API 参考

| 方法 | 说明 |
| --- | --- |
| `Init(ctx)` | 初始化引擎环境 |
| `Close()` | 释放引擎资源 |
| `LoadString(ctx, name, code)` | 编译 Expr 表达式并加入队列 |
| `Execute(ctx)` | 顺序求值队列中所有表达式，返回最后一个结果 |
| `ExecuteString(ctx, name, code)` | 编译并立即求值内联 Expr 表达式 |
| `RegisterGlobal(name, value)` | 注册全局变量到表达式环境 |
| `RegisterFunction(name, fn)` | 注册 Go 函数到表达式环境 |
| `CallFunction(ctx, name, args...)` | 命令式调用已注册的宿主函数 |
| `RegisterModule(name, module)` | 注册命名空间模块 |
| `StartWatch(ctx, key)` | 启动表达式热更新监听 |
| `StopWatch(key)` | 停止热更新监听 |

---

## 测试

```bash
cd expr && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [Expr 官方文档](https://github.com/expr-lang/expr)
- [Expr 语言参考](https://expr-lang.org/docs/language-definition)

## License

[MIT License](../LICENSE)
