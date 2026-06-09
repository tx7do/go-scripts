# GPython Engine · Python 脚本引擎

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-GPythonType-blue)](../types.go)

**基于 [gpython](https://github.com/go-python/gpython) 的纯 Go Python 3.4 子集脚本引擎**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

GPython 引擎使用 [go-python/gpython](https://github.com/go-python/gpython) 库，为 Go 应用提供 Python 3.4 子集的脚本执行能力。无需 CPython 依赖，纯 Go 实现，跨平台编译零障碍。

### 引擎特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/go-python/gpython` v0.2.0 |
| 语言版本 | Python 3.4 子集 |
| 标准库 | 内置 `sys`、`time`、`math` 等原生模块 |
| 线程安全 | 双锁模式（`RWMutex` + `execMutex`） |
| 热更新 | 支持 `StartWatch` / `StopWatch` |
| 类型常量 | `scriptEngine.GPythonType` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/gpython
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
    _ "github.com/tx7do/go-scripts/gpython" // 注册 GPython 引擎
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

    // 注入宿主变量
    _ = eng.RegisterGlobal("name", "world")

    // 注册宿主函数
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // 执行 Python 脚本
    result, _ := eng.ExecuteString(ctx, "hello.py", `greet(name)`)
    fmt.Println(result)
}
```

---

## API 参考

| 方法 | 说明 |
| --- | --- |
| `Init(ctx)` | 创建 gpython 解释器上下文和主模块 |
| `Close()` | 关闭解释器，释放资源 |
| `LoadString(ctx, name, code)` | 编译 Python 源码并加入执行队列 |
| `Execute(ctx)` | 顺序执行队列中所有脚本 |
| `ExecuteString(ctx, name, code)` | 编译并立即执行内联 Python 代码 |
| `RegisterGlobal(name, value)` | 注入 Go 值为 Python 全局变量 |
| `RegisterFunction(name, fn)` | 注册 Go 函数为 Python 可调用对象 |
| `CallFunction(ctx, name, args...)` | 调用 Python 函数 |
| `RegisterModule(name, module)` | 注册模块（键值映射为 `模块名_键名` 全局变量） |
| `StartWatch(ctx, key)` | 启动脚本热更新监听 |
| `StopWatch(key)` | 停止热更新监听 |

---

## 测试

```bash
cd gpython && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/gpython)

## License

[MIT License](../LICENSE)
