# Golang Embedded Script Engine

## 简介

这是一个用 Go 实现的脚本引擎，支持多种脚本语言（当前支持：Lua、JavaScript），旨在让宿主程序能够无缝加载并执行脚本以扩展行为或做快速原型开发。

## 特性

- 支持 Lua 脚本执行
- 支持 JavaScript 脚本执行
- 可嵌入到 Go 应用中以扩展运行时行为
- 提供单元测试与示例

## 需求

- Go 1.20+（根据 `go.mod` 调整）
- 可选：Lua runtime / JavaScript runtime 相关依赖（若使用 C 绑定或第三方引擎）

## 快速开始

### 1. 克隆仓库：

```bash
git clone https://github.com/tx7do/go-scripts.git
cd go-scripts
```

### 2. 安装依赖：

```bash
go mod tidy
```

## 使用JavaScript脚本引擎

```go
import (
	_ "github.com/tx7do/go-scripts/javascript"
	"github.com/tx7do/go-scripts"
)

// 初始化支持JavaScript的自动扩容引擎池（初始2个实例，最大10个）
enginePool, err := script_engine.NewAutoGrowEnginePool(2, 10, script_engine.JavaScriptType)
if err != nil {
    // 处理初始化错误
}
defer enginePool.Close()

// 定义Go中的业务函数
func updateUserStatus(userId int64, status string) error {
    // 实际更新用户状态的业务逻辑
return nil
}

// 注册到脚本引擎，供JavaScript调用
err := enginePool.RegisterFunction("updateUserStatus", updateUserStatus)
if err != nil {
    // 处理注册错误
}
```


## 使用Lua脚本引擎

```go
import (
	_ "github.com/tx7do/go-scripts/javascript"
	"github.com/tx7do/go-scripts"
)

// 初始化支持JavaScript的自动扩容引擎池（初始2个实例，最大10个）
enginePool, err := script_engine.NewAutoGrowEnginePool(2, 10, script_engine.LuaType)
if err != nil {
// 处理初始化错误
}
defer enginePool.Close()
```
