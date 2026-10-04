# Lua Host Bridges（gopher-lua 宿主模块桥）

把宿主能力以 `require("kratos_*")` 形态挂进 gopher-lua。提取自 go-wind-admin 脚本引擎的业务无关部分，与 [`hostmodule`](../../hostmodule/README.md)（语言无关 ModuleDef 框架）共享 HTTP 出站护栏实现。

## 五个模块

| require 名 | Go 侧入口 | 脚本侧函数面 |
|---|---|---|
| `kratos_logger` | `RegisterLogger(L, logger)` / `LoaderLogger(logger)` | `info` / `warn` / `error` / `debug` 及 `infof` / `warnf` / `errorf` / `debugf` |
| `kratos_crypto` | `RegisterCrypto(L, logger)` / `LoaderCrypto(logger)` | `encrypt` / `decrypt` / `is_encrypted` / `encrypt_json` / `decrypt_json`（go-utils/crypto 全局加密器）；`encrypt_payload` / `decrypt_payload` / `has_encrypted_payload`（AES-256-GCM 载荷封装：`task_id` / `task_type` 明文留外层供任务路由，其余字段加密） |
| `kratos_util` | `RegisterUtilAPI(L, logger)` / `LoaderUtil(logger, maxSleep...)` | `sleep`（默认上限 5s 可配）、`time` / `timestamp` / `date`；完整说明见 [UTIL_API.md](./UTIL_API.md) |
| `kratos_hook` | `RegisterHookAPI(L, engine, logger)` / `LoaderHook(engine, logger)` | `register` / `add_script` / `list`——宿主需实现 [`HookEngine`](hook.go) 接口（`RegisterHook` / `AddScript` / `ListHooks` / `RegisterCallback`）把脚本挂载接到自身存储，注册表本体见 [`go-scripts/hook`](../../hook/README.md) |
| `kratos_http` | `RegisterHTTP(L, opts)` / `LoaderHTTP(opts)` | `request(url, options?)` / `get(url)` / `post(url, body, contentType?)` → `{status, body, headers}`；`opts` 为 [`hostmodule.HTTPOptions`](../../hostmodule/README.md)，护栏（fail-closed 白名单 / 环回硬禁 / 重定向逐跳复检 / 1MB / 超时）与 JS 侧完全一致 |

## 两套入口

- `Register*(L, ...)`：直接在指定 `LState` 上 `PreloadModule`，适合自管 VM 的宿主。
- `Loader*(...)`：返回 `lua.LGFunction` loader，适合引擎层统一注册。

## 用法

```go
import (
    lua "github.com/yuin/gopher-lua"

    "github.com/tx7do/go-scripts/hostmodule"
    "github.com/tx7do/go-scripts/lua/host"
)

L := lua.NewState()
defer L.Close()

host.RegisterLogger(L, logger)                              // logger: kratos-bootstrap/logger Helper
host.RegisterHTTP(L, hostmodule.HTTPAllowlistFromEnv())     // 环境变量 SCRIPT_HTTP_ALLOWED_DOMAINS
```

```lua
local log  = require("kratos_logger")
local http = require("kratos_http")

log.info("hello from lua")
local resp = http.get("https://hooks.slack.com/services/xxx")
if resp.status == 200 then
    log.infof("got %d bytes", #resp.body)
end
```

## 依赖

- [`github.com/yuin/gopher-lua`](https://github.com/yuin/gopher-lua) —— Lua 5.1 虚拟机
- [`github.com/tx7do/go-scripts/hostmodule`](../../hostmodule/README.md)、[`github.com/tx7do/go-scripts/lua/convert`](../convert/README.md)
- [`github.com/tx7do/go-utils/crypto`](https://github.com/tx7do/go-utils)、[`github.com/tx7do/kratos-bootstrap/logger`](https://github.com/tx7do/kratos-bootstrap)
