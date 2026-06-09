# Yaegi Engine · Go 脚本引擎

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org) [![Engine Type](https://img.shields.io/badge/type-YaegiType-blue)](../types.go)

**基于 [Traefik Yaegi](https://github.com/traefik/yaegi) 的原生 Go 脚本引擎**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Yaegi 引擎使用 [Traefik Yaegi](https://github.com/traefik/yaegi) 库，为 Go 应用提供原生 Go 语言的运行时脚本执行能力。无需编译，直接解释执行 Go 源码，适用于动态插件、DevOps 工具链等场景。

### 引擎特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/traefik/yaegi` v0.16.1 |
| 语言版本 | Go 1.x |
| 宿主互操作 | 通过合成 `host` 包暴露全局变量和函数，脚本需 `import "host"` |
| 线程安全 | 双锁模式（`RWMutex` + `execMutex`） |
| 热更新 | 支持 `StartWatch` / `StopWatch` |
| 类型常量 | `scriptEngine.YaegiType` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/yaegi
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
    _ "github.com/tx7do/go-scripts/yaegi" // 注册 Yaegi 引擎
)

func main() {
    eng, err := scriptEngine.NewScriptEngine(scriptEngine.YaegiType)
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close()

    ctx := context.Background()
    eng.Init(ctx)

    // 注册宿主函数（脚本通过 host.FuncName 调用）
    _ = eng.RegisterFunction("greet", func(name string) string {
        return fmt.Sprintf("Hello, %s!", name)
    })

    // 执行 Go 脚本（注意：需要 import "host"）
    result, _ := eng.ExecuteString(ctx, "hello.go", `
        import "host"
        host.greet("world")
    `)
    fmt.Println(result) // Hello, world!
}
```

> **注意**：Yaegi 引擎中，通过 `RegisterGlobal` / `RegisterFunction` 注册的变量和函数被放在合成的 `host` 包中，脚本必须 `import "host"` 才能访问。

---

## API 参考

| 方法 | 说明 |
| --- | --- |
| `Init(ctx)` | 创建 Yaegi 解释器实例 |
| `Close()` | 关闭解释器，释放资源 |
| `LoadString(ctx, name, code)` | 将 Go 源码加入执行队列 |
| `Execute(ctx)` | 顺序执行队列中所有脚本 |
| `ExecuteString(ctx, name, code)` | 编译并立即执行内联 Go 代码 |
| `RegisterGlobal(name, value)` | 在 `host` 包中注册全局变量 |
| `RegisterFunction(name, fn)` | 在 `host` 包中注册 Go 函数 |
| `CallFunction(ctx, name, args...)` | 调用已注册的函数 |
| `RegisterModule(name, module)` | 注册自定义包供脚本 `import` |
| `StartWatch(ctx, key)` | 启动脚本热更新监听 |
| `StopWatch(key)` | 停止热更新监听 |

---

## 测试

```bash
cd yaegi && go test -v ./...
```

---

## 相关文档

- [返回主文档](../README.md)
- [Yaegi 官方文档](https://github.com/traefik/yaegi)
- [Go Reference](https://pkg.go.dev/github.com/tx7do/go-scripts/yaegi)

## License

[MIT License](../LICENSE)
