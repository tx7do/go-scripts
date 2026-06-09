# JavaScript 脚本引擎

基于 [goja](https://github.com/dop251/goja) 封装的 Go 嵌入式 JavaScript 脚本引擎实现，作为 [`go-scripts`](../) 的子模块对外提供。

## 设计要点

- **统一接口**：实现 [`script_engine.Engine`](../engine.go) 接口，可与 [`Manager`](../manager.go)、[`EnginePool`](../engine_pool.go)、[`AutoGrowEnginePool`](../engine_pool_autogrow.go) 等根模块组件无缝配合。
- **工厂自动注册**：通过 `init()` 注册到 [`script_engine`](../factory.go) 全局工厂表，只需 `import _ "github.com/tx7do/go-scripts/javascript"` 即可启用。
- **并发安全**：内部用 `sync.RWMutex` + `execMu` 双锁保护 runtime / programs / source；`Execute*` 系列通过 `goja.Runtime.Interrupt` 支持 `ctx.Done()` 中断。
- **脚本源解耦**：通过 `Source` 接口（`FileSource` / `MemSource` / `MultiSource` / 自定义扩展）注入脚本来源，engine 本身不再耦合任何 IO 细节。
- **Node.js 兼容层**：内置启用 `require` / `console` / `process` 三个 goja_nodejs 模块，可直接使用 CommonJS 风格的模块加载。
- **ES6 子集支持**：goja 支持 ES5 全部 + ES6 部分语法（`let` / `const` / 模板字符串 / 箭头函数 / 解构等）。

## 依赖

- Go 1.24+
- [`github.com/dop251/goja`](https://github.com/dop251/goja)
- [`github.com/dop251/goja_nodejs`](https://github.com/dop251/goja_nodejs)（提供 `require` / `console` / `process`）
- [`github.com/tx7do/go-scripts`](../)（根模块）

## 快速开始

### 1. 引入模块

```go
import (
    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/javascript" // 注册 JavaScript 工厂
)
```

只要触发 `init()`，后续所有 `scriptEngine.NewScriptEngine(scriptEngine.JavaScriptType, ...)` 调用都会返回本模块的 engine 实例。

### 2. 单实例使用

```go
eng, err := scriptEngine.NewScriptEngine(scriptEngine.JavaScriptType)
if err != nil {
    log.Fatal(err)
}
defer eng.Close()

ctx := context.Background()
if err := eng.Init(ctx); err != nil {
    log.Fatal(err)
}

// 注入变量（host -> JS）
_ = eng.RegisterGlobal("answer", 42)

// 注入函数（host -> JS）
_ = eng.RegisterFunction("sayHello", func(name string) {
    fmt.Println("Hello,", name)
})

// 执行内联脚本
result, err := eng.ExecuteString(ctx, "demo.js", `
    sayHello("world");
    answer + 100;
`)
// result = 142
```

### 3. 引擎池（推荐生产用法）

```go
// 固定大小池
pool, err := scriptEngine.NewEnginePool(8, scriptEngine.JavaScriptType)

// 或：可自增池（初始 2，上限 16）
pool, err := scriptEngine.NewAutoGrowEnginePool(2, 16, scriptEngine.JavaScriptType)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

ctx := context.Background()
_, _ = pool.ExecuteString(ctx, "init.js", `globalThis.appName = "demo";`)
```

### 4. 配合 ScriptSource（统一脚本来源）

```go
// 本地文件 + mtime 热更新检测
src := scriptEngine.NewFileSource()
eng.SetSource(src)

// 从 Source 加载并执行
result, err := eng.ExecuteFromKey(ctx, "/path/to/script.js")

// 也可以直接走 engine pool 的 wrapper
pool.SetSource(src)
results, err := pool.ExecuteFromKeys(ctx, []string{"a.js", "b.js"})
```

也可使用 `MemSource`（纯内存，零 IO）或 `MultiSource`（多源聚合 / fallback）。

## 核心 API

`Engine` 接口提供的方法如下（节选，完整定义见 [`engine.go`](../engine.go)）：

### 生命周期

| 方法 | 说明 |
|---|---|
| `Init(ctx)` | 初始化 runtime，必须在任何 Load*/Execute* 之前调用 |
| `Close()` | 释放 runtime，调用后需重新 Init 才能复用 |
| `IsInitialized()` | 查询初始化状态 |

### ScriptSource

| 方法 | 说明 |
|---|---|
| `SetSource(source)` | 绑定脚本源（FileSource / S3 / Mem / Multi / ...），传 `nil` 清除绑定 |
| `GetSource()` | 取回当前绑定的 Source（未绑定返回 `nil`） |

### 脚本加载

| 方法 | 说明 |
|---|---|
| `Load(ctx, key)` | 从绑定的 Source 加载单个脚本（`key` 为路径 / object key / 脚本 ID） |
| `LoadMulti(ctx, keys)` | 批量加载，遇到第一个错误即中止 |
| `LoadString(ctx, name, code)` | 直接编译内联脚本，**不走 Source**。`name` 用于诊断（出现在 stack trace 里） |

### 脚本执行

| 方法 | 说明 |
|---|---|
| `Execute(ctx)` | 执行所有已 `Load*` 的脚本，按顺序返回结果 |
| `ExecuteFromKey(ctx, key)` | 从 Source 加载 + 立即执行，一步到位 |
| `ExecuteFromKeys(ctx, keys)` | 多 key 版本，结果顺序与 `keys` 一致 |
| `ExecuteString(ctx, name, code)` | 编译并立即执行内联脚本，**不走 Source** |

### 全局变量 / 函数 / 模块

| 方法 | 说明 |
|---|---|
| `RegisterGlobal(name, value)` | 注册或覆盖 JS 全局变量（任何 Go 值，由 goja 自动转换） |
| `GetGlobal(name)` | 读取 JS 全局变量，未定义时返回 error |
| `RegisterFunction(name, fn)` | 注册宿主函数，脚本可按 `name` 直接调用 |
| `CallFunction(ctx, name, args...)` | 调用脚本中名为 `name` 的函数，支持 `ctx` 超时 / 中断 |
| `RegisterModule(name, module)` | 注册模块。`module` 可为 `map[string]any`（导出多个成员）或单个值 |

### 错误处理

| 方法 | 说明 |
|---|---|
| `GetLastError()` | 取回最近一次发生的错误 |
| `ClearError()` | 清除最近错误状态 |

## 宿主与脚本互操作

### 变量

支持三种访问模式：

- **单向只写**：通过 `RegisterGlobal` 把宿主变量注入脚本。
- **单向只读**：通过 `GetGlobal` 读取脚本中定义的变量。
- **双向可读可写**：传入指针 / 结构体引用，脚本对其字段的修改会反映到宿主（goja 自动桥接）。

### 函数

- **脚本调用宿主**：`RegisterFunction` 后，脚本中直接按名调用。
- **宿主调用脚本**：先 `ExecuteString` 定义函数（如 `function add(a,b){return a+b}`），再 `CallFunction(ctx, "add", 1, 2)`。

### 模块

`RegisterModule(name, module)` 用于把一组相关变量 / 函数封装到一个命名空间下，避免污染全局栈：

```go
_ = eng.RegisterModule("mathutil", map[string]any{
    "square": func(x float64) float64 { return x * x },
    "pi":     3.14159,
})
```

```javascript
// 脚本侧
mathutil.square(mathutil.pi * 2)
```

### CommonJS 模块（`require`）

本模块在初始化时启用了 `goja_nodejs` 的 `require` / `console` / `process`，因此脚本中可以使用标准 Node.js 风格的模块：

```javascript
// script/m.js
function sayHi(user) {
    console.log(`Js module say Hello, ${user}!`);
}
module.exports = { sayHi: sayHi };
```

```javascript
// script/main.js
var m = require("./script/m.js");
m.sayHi("Tom");
```

`require` 默认按相对路径（相对于当前工作目录或脚本所在目录）解析，参见 [goja_nodejs require](https://github.com/dop251/goja_nodejs)。

## 完整示例

```go
package main

import (
    "context"
    "fmt"
    "log"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/javascript"
)

type User struct {
    Name  string
    Token string
}

func main() {
    pool, err := scriptEngine.NewEnginePool(4, scriptEngine.JavaScriptType)
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    ctx := context.Background()

    // 1. 注入结构体（脚本可修改其字段）
    u := &User{Name: "Tim"}
    _ = pool.RegisterGlobal("u", u)

    // 2. 注入宿主函数
    _ = pool.RegisterFunction("sayHello", func(name string) {
        fmt.Println("Hello,", name)
    })

    // 3. 注入模块
    _ = pool.RegisterModule("config", map[string]any{
        "env":  "prod",
        "port": 8080,
    })

    // 4. 执行内联脚本
    _, err = pool.ExecuteString(ctx, "app.js", `
        sayHello(u.Name);
        u.Token = "abcd-1234";
        console.log("env:", config.env, "port:", config.port);
    `)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("user token:", u.Token) // abcd-1234
}
```

## 测试

```bash
cd javascript
go test -v ./...
```

涵盖：

- 基础执行 + 全局变量读写 + 宿主函数注入
- 并发 `ExecuteString` / `CallFunction` 压测
- 并发 `Init` / `Close` / `Execute` 压测
- `Source` 注入 + `Load` / `LoadMulti` / `ExecuteFromKey` / `ExecuteFromKeys`
- `FileSource` 端到端（`t.TempDir()` + 临时脚本文件）

## 相关文档

- 根模块 README：[../README.md](../README.md)
- Engine 接口定义：[../engine.go](../engine.go)
- ScriptSource 实现：[../source.go](../source.go)
- 引擎池：[../engine_pool.go](../engine_pool.go) / [../engine_pool_autogrow.go](../engine_pool_autogrow.go)
- goja 文档：https://github.com/dop251/goja
