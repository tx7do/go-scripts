# Hook Registry（钩子点注册表）

语言无关的钩子注册表：宿主程序声明"钩子点"（如 `user.after_create`），脚本按名称挂载到钩子上，触发时按优先级依次执行。提取自 go-wind-admin 脚本引擎，零业务依赖，可独立嵌入任意宿主。

## 组成

| 类型 | 说明 |
|---|---|
| `Hook` | 钩子点：`Name` / `Description` / `Scripts` |
| `Script` | 挂载的脚本元数据：`ID` / `Name` / `Hook` / `Source` / `Enabled` / `Priority` / `Description` / `Version` / `Author` / `Critical` |
| `Registry` | 注册表本体，`sync.RWMutex` 并发安全 |

## API 要点

- `RegisterHook(name, description)`：注册钩子点，重名报错。
- `AddScript(hookName, script)`：**upsert 语义**——同名脚本直接替换旧脚本，这正是热更新（源变更后重新注册）的机制；钩子不存在时自动注册。
- 脚本表始终按 `Priority` **升序**保持排序（数值小者先执行）。
- `GetHook` / `GetScripts` / `ListHooks`（排序后的钩子名）/ `GetAllHooks` / `RemoveScript` / `Clear` / `Count` / `ScriptCount`。

## 用法

```go
reg := hook.NewRegistry()

_ = reg.RegisterHook("user.after_create", "用户创建后触发")
_ = reg.AddScript("user.after_create", &hook.Script{
    Name:     "welcome",
    Hook:     "user.after_create",
    Source:   "-- lua/js 源码或引用标识",
    Enabled:  true,
    Priority: 10, // 数值小者先执行
})

for _, s := range reg.GetScripts("user.after_create") {
    // 按 Priority 顺序取出脚本，交给你选择的脚本引擎执行
    _ = s
}
```

## 与语言运行时的关系

本包只管"注册表"这一层，不关心脚本用什么语言执行。gopher-lua 侧的挂载适配（`kratos_hook` require 模块与 `HookEngine` 接口）见 [`lua/host`](../lua/host/README.md)；宿主实现 `HookEngine` 即可把本注册表接到任意脚本引擎。

## 依赖

- Go 1.24+（仅标准库）
