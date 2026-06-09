# Lua 脚本引擎

基于 [GopherLua](https://github.com/yuin/gopher-lua) 封装的 Go 嵌入式 Lua 脚本引擎实现，作为 [`go-scripts`](../) 的子模块对外提供。

GopherLua 是纯 Go 实现的 Lua 5.1 虚拟机，与 C 风格的 Lua C API 风格接近，性能不错且零 CGO 依赖。

## 设计要点

- **统一接口**：实现 [`script_engine.Engine`](../engine.go) 接口，可与 [`Manager`](../manager.go)、[`EnginePool`](../engine_pool.go)、[`AutoGrowEnginePool`](../engine_pool_autogrow.go) 等根模块组件无缝配合。
- **工厂自动注册**：通过 `init()` 注册到 [`script_engine`](../factory.go) 全局工厂表，只需 `import _ "github.com/tx7do/go-scripts/lua"` 即可启用。
- **并发安全**：内部用 `sync.RWMutex` 保护 VM / source / initialized；`Execute*` / `CallFunction` 通过 channel + `ctx.Done()` 支持取消和超时。
- **LState 复用池**：本模块自带 [`statePool`](state_pool.go)，在 engine `Init` 时借出、`Close` 时归还，默认上限 10 个 LState 实例，避免每次脚本运行都重建虚拟机。
- **脚本源解耦**：通过 `Source` 接口（`FileSource` / `MemSource` / `MultiSource` / 自定义扩展）注入脚本来源，engine 本身不再耦合任何 IO 细节。
- **标准 Lua 生态**：`Init` 时自动开启 Lua 标准库 + [`gopher-lua-libs`](https://github.com/vadv/gopher-lua-libs)（json / http / regexp / db / time / ...）+ [`gluacrypto`](https://github.com/tengattack/gluacrypto)（加密 / 哈希）+ `GetLuaPath` 辅助函数。

## gopher-lua 的几点限制（务必留意）

| 限制 | 表现 | 应对策略 |
|---|---|---|
| **单编译单元** | LState 一次只保留一个编译后的 `LFunction`，连续 `Load` / `LoadString` 会覆盖 | 多脚本用 `ExecuteFromKeys` 或手动 `Load` + `Execute` 配对 |
| **5.1 子集** | 不支持 Lua 5.2+ 的 `goto` / bit32 / 5.3 整数类型 | 写脚本时按 Lua 5.1 语法 |
| **无 JIT** | 性能远不及 LuaJIT，但比 C Lua 略慢 | 高频热路径建议预编译或下沉到 Go |
| **`LoadString` / `DoString` 没有 name 参数** | 本模块的 `LoadString(ctx, name, code)` / `ExecuteString(ctx, name, code)` 中的 `name` 仅做接口兼容，**会被忽略** | stack trace 中无法看到脚本名 |

## 依赖

- Go 1.24+
- [`github.com/yuin/gopher-lua`](https://github.com/yuin/gopher-lua) —— Lua 5.1 虚拟机
- [`layeh.com/gopher-luar`](https://github.com/layeh/gopher-luar) —— Go ↔ Lua 双向值桥接
- [`github.com/yuin/gluamapper`](https://github.com/yuin/gluamapper) —— Lua table ↔ Go struct 映射
- [`github.com/vadv/gopher-lua-libs`](https://github.com/vadv/gopher-lua-libs) —— 通用库扩展（json/http/regexp/db/...）
- [`github.com/tengattack/gluacrypto`](https://github.com/tengattack/gluacrypto) —— 加密 / 哈希扩展
- [`github.com/tx7do/go-scripts`](../) —— 根模块

## 快速开始

### 1. 引入模块

```go
import (
    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua" // 注册 Lua 工厂
)
```

只要触发 `init()`，后续所有 `scriptEngine.NewScriptEngine(scriptEngine.LuaType, ...)` 调用都会返回本模块的 engine 实例。

### 2. 单实例使用

```go
eng, err := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
if err != nil {
    log.Fatal(err)
}
defer eng.Close()

ctx := context.Background()
if err := eng.Init(ctx); err != nil {
    log.Fatal(err)
}

// 注入变量（host -> Lua）
_ = eng.RegisterGlobal("answer", 42)

// 注入宿主函数（必须是 Lua.LGFunction）
_ = eng.RegisterFunction("say_hello", func(L *lua.LState) int {
    name := L.CheckString(1)
    fmt.Println("Hello,", name)
    return 0 // 返回值个数
})

// 执行内联脚本
_, err = eng.ExecuteString(ctx, "demo.lua", `
    say_hello("world")
    print(answer + 100)
`)
```

### 3. 引擎池（推荐生产用法）

```go
// 固定大小池
pool, err := scriptEngine.NewEnginePool(8, scriptEngine.LuaType)

// 或：可自增池（初始 2，上限 16）
pool, err := scriptEngine.NewAutoGrowEnginePool(2, 16, scriptEngine.LuaType)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

ctx := context.Background()
_, _ = pool.ExecuteString(ctx, "init.lua", `app_name = "demo"`)
```

### 4. 配合 ScriptSource（统一脚本来源）

```go
// 本地文件 + mtime 热更新检测
src := scriptEngine.NewFileSource()
eng.SetSource(src)

// 从 Source 加载并执行
_, err := eng.ExecuteFromKey(ctx, "/path/to/script.lua")

// 也可以直接走 engine pool 的 wrapper
pool.SetSource(src)
results, err := pool.ExecuteFromKeys(ctx, []string{"a.lua", "b.lua"})
```

也可使用 `MemSource`（纯内存，零 IO）或 `MultiSource`（多源聚合 / fallback）。

## 核心 API

`Engine` 接口提供的方法如下（节选，完整定义见 [`engine.go`](../engine.go)）：

### 生命周期

| 方法 | 说明 |
|---|---|
| `Init(ctx)` | 从 `statePool` 借出 LState 并开启标准库；必须在任何 Load*/Execute* 之前调用 |
| `Close()` | 归还 LState 到池中，调用后需重新 Init 才能复用 |
| `IsInitialized()` | 查询初始化状态 |

### ScriptSource

| 方法 | 说明 |
|---|---|
| `SetSource(source)` | 绑定脚本源（FileSource / S3 / Mem / Multi / ...），传 `nil` 清除绑定 |
| `GetSource()` | 取回当前绑定的 Source（未绑定返回 `nil`） |

### 脚本加载

| 方法 | 说明 |
|---|---|
| `Load(ctx, key)` | 从绑定的 Source 加载单个脚本。**注意**：gopher-lua 一次只保留 1 个编译函数，连续 Load 会互相覆盖 |
| `LoadMulti(ctx, keys)` | 批量加载，遇到第一个错误即中止。同上覆盖规则 |
| `LoadString(ctx, name, code)` | 直接编译内联脚本，**不走 Source**。`name` 被忽略（gopher-lua 不支持） |

### 脚本执行

| 方法 | 说明 |
|---|---|
| `Execute(ctx)` | 执行上一次 `Load*` 编译的函数，ctx 中止会通过 channel 取消 |
| `ExecuteFromKey(ctx, key)` | 从 Source 加载 + 立即执行，一步到位 |
| `ExecuteFromKeys(ctx, keys)` | 多 key 版本，结果顺序与 `keys` 一致（推荐用法） |
| `ExecuteString(ctx, name, code)` | 编译并立即执行内联脚本，**不走 Source**。`name` 被忽略 |

### 全局变量 / 函数 / 模块

| 方法 | 说明 |
|---|---|
| `RegisterGlobal(name, value)` | 把 Go 值通过 `luar` 桥接到 Lua 全局；支持基本类型、map、struct 指针（**字段可双向读写**） |
| `GetGlobal(name)` | 读取 Lua 全局变量，自动转 `interface{}`（LNumber → int64/float64、LTable → map/slice） |
| `RegisterFunction(name, fn)` | 注册 Lua 函数。`fn` **必须是** `Lua.LGFunction` 类型，否则返回 error |
| `CallFunction(ctx, name, args...)` | 调用 Lua 中名为 `name` 的函数，参数自动转 LValue，返回值自动转 Go 值 |
| `RegisterModule(name, module)` | 注册 Lua 模块。`module` **必须是** `Lua.LGFunction` 类型（模块 loader 函数） |

### 错误处理

| 方法 | 说明 |
|---|---|
| `GetLastError()` | 取回最近一次发生的错误 |
| `ClearError()` | 清除最近错误状态 |

## 宿主与脚本互操作

### 变量

支持三种访问模式：

- **单向只写**：`RegisterGlobal` 把宿主变量注入脚本（基本类型 / map / slice）。
- **单向只读**：`GetGlobal` 读取脚本中定义的变量。
- **双向可读可写**：通过 `luar` 注入**结构体指针**，脚本对其字段的修改会反映到宿主。

```go
type User struct {
    Name  string
    Token string
}

u := &User{Name: "Tim"}
_ = eng.RegisterGlobal("u", u)
_, _ = eng.ExecuteString(ctx, "", `u:SetToken("abcd")`)
fmt.Println(u.Token) // abcd
```

### 函数

**Lua 函数签名要求**：本模块的 `RegisterFunction` / `RegisterModule` **只接受** `Lua.LGFunction`（即 `func(*lua.LState) int`），其它类型会报错。

```go
import Lua "github.com/yuin/gopher-lua"

// host -> Lua
_ = eng.RegisterFunction("add", func(L *Lua.LState) int {
    a := L.CheckInt(1)
    b := L.CheckInt(2)
    L.Push(Lua.LNumber(a + b))
    return 1 // 返回值个数
})

// Lua -> host
_, _ = eng.ExecuteString(ctx, "", `result = add(10, 20)`)
v, _ := eng.GetGlobal("result") // int64(30)
```

宿主调用脚本中定义的函数：

```go
_, _ = eng.ExecuteString(ctx, "", `
    function multiply(a, b) return a * b end
`)
v, _ := eng.CallFunction(ctx, "multiply", 3, 4) // int64(12)
```

### 模块

`RegisterModule` 注册的是 Lua 模块 loader 函数（`Lua.LGFunction` 类型）：

```go
modLoader := func(L *Lua.LState) int {
    mod := L.NewTable()
    L.SetField(mod, "pi", Lua.LNumber(3.14159))
    L.SetField(mod, "square", L.NewFunction(func(L *Lua.LState) int {
        x := L.CheckNumber(1)
        L.Push(Lua.LNumber(x * x))
        return 1
    }))
    L.Push(mod)
    return 1
}
_ = eng.RegisterModule("mathutil", modLoader)
```

```lua
-- 脚本侧
print(mathutil.pi)               -- 3.14159
print(mathutil.square(2))        -- 4
```

> 注意：上面的 `RegisterModule` 走的是 gopher-lua 自带的 loader 协议（`L.NewFunction + L.Call`），与 `script/test_module.lua` 中展示的"`module` 表 + `return module`"写法用于 **`require` 加载场景**。两者并不冲突，可以并存。

### Lua 内置模块（已自动启用）

`virtualMachine.init()` 时自动开启以下库：

| 库 | 来源 | 用途 |
|---|---|---|
| `string` / `table` / `math` / `io` / `os` / `debug` / `package` | gopher-lua 标准库 | Lua 5.1 内置 |
| `json` | gopher-lua-libs | JSON 编解码 |
| `http` | gopher-lua-libs | HTTP 客户端 |
| `regexp` | gopher-lua-libs | 正则匹配（基于 Go regexp） |
| `db` | gopher-lua-libs | MySQL / SQLite / PostgreSQL 访问 |
| `time` | gopher-lua-libs | 时间操作 |
| `crypto` | gluacrypto | md5 / sha / hmac / aes / des / rsa 等 |
| `GetLuaPath()` | 本模块自定义 | 返回当前可执行文件目录下的 `script` 子目录，方便拼接 `package.path` |

`require` 标准 Lua 写法可用：

```lua
-- script/test_module.lua
module = {}
module.constant = "这是一个常量"
function module.func3() print("hello") end
return module

-- script/main.lua
local m = require("test_module")
print(m.constant)
m.func3()
```

## 完整示例

```go
package main

import (
    "context"
    "fmt"
    "log"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua"
    Lua "github.com/yuin/gopher-lua"
)

type User struct {
    Name  string
    Token string
}

func main() {
    pool, err := scriptEngine.NewEnginePool(4, scriptEngine.LuaType)
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    ctx := context.Background()

    // 1. 注入结构体（脚本可修改其字段）
    u := &User{Name: "Tim"}
    _ = pool.RegisterGlobal("u", u)

    // 2. 注入宿主函数（必须是 Lua.LGFunction）
    _ = pool.RegisterFunction("say_hello", func(L *Lua.LState) int {
        name := L.CheckString(1)
        fmt.Println("Hello,", name)
        return 0
    })

    // 3. 执行内联脚本
    _, err = pool.ExecuteString(ctx, "app.lua", `
        say_hello(u.Name)
        u.Token = "abcd-1234"
        print("answer:", 6 * 7)
    `)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("user token:", u.Token) // abcd-1234
}
```

## LState 状态池

本模块内部维护一个 [`statePool`](state_pool.go)：

- `Borrow()` —— 取空闲 LState，无则新建
- `Return(L)` —— 归还 LState，超过上限则 `Close`
- `Shutdown()` —— 关闭池，回收所有空闲 LState（仅用于全局清理，正常业务无需调用）

默认配置：

| 项 | 默认值 |
|---|---|
| `maxSaved` | 10 |
| `CallStackSize` | 4096 |
| `RegistrySize` | 4096 |
| `SkipOpenLibs` | true（每个 VM 在 `Init` 时单独 `OpenLibs`，避免污染池） |

可通过 `newStatePoolWithOptions(Lua.Options{...})` 自定义。

## 测试

```bash
cd lua
go test -v ./...
```

涵盖：

- 基础执行 + 全局变量读写 + 宿主函数注入
- 并发 `CallFunction` + `GetGlobal` 压测（50 goroutine × 200 loop）
- 并发 `Init` / `Close` 压测（40 goroutine × 200 op）
- `Source` 注入 + `Load` / `LoadMulti` / `ExecuteFromKey` / `ExecuteFromKeys`
- `FileSource` 端到端（`t.TempDir()` + 临时脚本文件）
- `statePool` `Borrow` / `Return` 基础功能
- Lua table ↔ Go struct 映射（`gluamapper`）

## 相关文档

- 根模块 README：[../README.md](../README.md)
- Engine 接口定义：[../engine.go](../engine.go)
- ScriptSource 实现：[../source.go](../source.go)
- 引擎池：[../engine_pool.go](../engine_pool.go) / [../engine_pool_autogrow.go](../engine_pool_autogrow.go)
- gopher-lua 文档：https://github.com/yuin/gopher-lua
- gopher-lua-libs 文档：https://github.com/vadv/gopher-lua-libs
- gluacrypto 文档：https://github.com/tengattack/gluacrypto
