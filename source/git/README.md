# Git Source · Git 脚本来源

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**基于 Git 仓库的脚本来源，支持 commit hash 比对热更新检测**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Git Source 使用 [go-git](https://github.com/go-git/go-git) 从远程 Git 仓库或本地 Git 仓库读取脚本。在初始化时自动 Clone 仓库到本地临时目录，通过 commit hash 轮询检测热更新。

### 特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | `github.com/go-git/go-git/v5`（纯 Go，零 CGO） |
| 热更新 | commit hash 比对轮询（`git pull`） |
| 认证 | 支持 Token / 用户名密码 / SSH Key |
| 浅克隆 | 支持 `WithDepth` 限制克隆深度 |
| 本地仓库 | 支持 `WithLocalPath` 直接打开本地仓库 |
| Key 前缀 | 支持 `WithPrefix` 命名空间隔离 |
| 接口 | 实现 `source.ReadWatcher` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/source/git
```

---

## 配置选项

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `WithRepoURL(url)` | **必填*** | 远程 Git 仓库 URL |
| `WithBranch(branch)` | `HEAD` | 分支 / 标签 / 引用名 |
| `WithPrefix(prefix)` | 空 | Key 前缀（自动去除前导 `/`） |
| `WithLocalPath(path)` | 空 | 本地仓库路径（与 RepoURL 二选一） |
| `WithAuth(user, pass)` | 空 | 用户名 + 密码认证 |
| `WithToken(token)` | 空 | Bearer Token（GitHub PAT / GitLab Token） |
| `WithSSHKey(path)` | 空 | SSH 私钥文件路径 |
| `WithDepth(n)` | 0（完整克隆） | 浅克隆深度 |
| `WithPullInterval(d)` | 30s | Watch 轮询间隔 |

> \* 当使用 `WithLocalPath` 时不需要 `WithRepoURL`。

---

## 快速开始

### 从远程仓库加载

```go
package main

import (
    "context"
    "fmt"
    gitSrc "github.com/tx7do/go-scripts/source/git"
)

func main() {
    ctx := context.Background()

    src, err := gitSrc.New(ctx,
        gitSrc.WithRepoURL("https://github.com/user/scripts.git"),
        gitSrc.WithBranch("main"),
        gitSrc.WithPrefix("scripts/lua/"),
    )
    if err != nil { panic(err) }
    defer src.Close()

    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

### 使用 Token 认证（GitHub）

```go
src, err := gitSrc.New(ctx,
    gitSrc.WithRepoURL("https://github.com/my-org/scripts.git"),
    gitSrc.WithToken("ghp_xxxxxxxxxxxx"),
    gitSrc.WithDepth(1),  // 浅克隆
)
```

### 使用本地仓库

```go
src, err := gitSrc.New(ctx,
    gitSrc.WithLocalPath("/path/to/local/repo"),
    gitSrc.WithPrefix("lua/"),
)
```

### 热更新监听

```go
// 先 Load 建立基线
_, _ = src.Load(ctx, "main.lua")

// 启动监听（定期 git pull + 比较 HEAD hash）
ch, _ := src.Watch(ctx, "main.lua")

for range ch {
    // 远程仓库有新 commit
    code, _ := src.Load(ctx, "main.lua")
    fmt.Println("reloaded:", code)
}
```

---

## 热更新机制

```
每 30s（默认）:
  git pull
    ↓
  获取当前 HEAD commit hash
    ↓
  与 Load 时记录的 baseline hash 比对
    ↓
  不同 → 发送变更信号
```

- **每个 Watcher 独立追踪**：每个 `Watch()` 调用维护自己的 baseline，多个 Watcher 不会互相干扰。
- **Pull 失败容错**：网络异常等导致 Pull 失败时，跳过当前轮次，下次重试。
- **无变更无信号**：HEAD hash 未变时不发送信号。

---

## 错误处理

| 错误 | 说明 |
| --- | --- |
| `ErrNotFound` | 文件不存在；用 `errors.Is(err, gitSrc.ErrNotFound)` 或 `gitSrc.IsNotFound(err)` 识别 |
| 其它 | 包装原始错误，前缀为 `git source: ...` |

---

## 测试

```bash
cd source/git && go test -v ./...
```

测试覆盖（25 个用例全部通过，不依赖真实 Git 服务器）：

| 类别 | 用例 |
| --- | --- |
| 接口实现 | 编译期断言 |
| 构造校验 | `WithRepoURL` 或 `WithLocalPath` 必填 |
| Prefix 规范化 | 6 种 prefix 写法表驱动测试 |
| Load | 正常加载 / 文件不存在（ErrNotFound）/ Prefix / Context 取消 |
| Watch | 有变更触发 / 无变更不触发 / Context 取消 / 未 Load 报错 / Pull 失败容错 / 并发多 Watcher |
| 并发安全 | 30 goroutine 并发 Load |
| Options | WithRepoURL / WithBranch / WithAuth / WithToken / WithDepth / WithPullInterval |
| 本地仓库 | 从本地目录加载 |

---

## 相关文档

- [返回 Source 文档](../README.md)
- [返回主文档](../../README.md)

## License

[MIT License](../../LICENSE)
