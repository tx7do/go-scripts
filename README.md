# Golang Embedded Script Engine

简介
---

这是一个用 Go 实现的脚本引擎，支持多种脚本语言（当前支持：Lua、JavaScript），旨在让宿主程序能够无缝加载并执行脚本以扩展行为或做快速原型开发。

特性
---

- 支持 Lua 脚本执行
- 支持 JavaScript 脚本执行
- 可嵌入到 Go 应用中以扩展运行时行为
- 提供单元测试与示例

需求
---

- Go 1.20+（根据 `go.mod` 调整）
- 可选：Lua runtime / JavaScript runtime 相关依赖（若使用 C 绑定或第三方引擎）

快速开始
---

1. 克隆仓库：
```bash
git clone https://github.com/tx7do/go-scripts.git
cd go-scripts
```
