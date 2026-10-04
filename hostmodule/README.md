# Host Module Framework（语言无关宿主模块框架）

[`ModuleDef`](module.go) = 模块名 + `map[函数名]Go函数`，是向脚本 VM 注入宿主能力的语言无关形态：

- **JS（goja）**：把 `ModuleDef.Funcs` 直接注册进 VM，Go 函数即可被脚本调用；`(T, error)` 返回形态自动转为 JS 异常。
- **Lua（gopher-lua）**：继续走 [`lua/host`](../lua/host/README.md) 的 `Loader*` 形态；本包的 HTTP 护栏原语两侧行为完全一致。

Go 函数签名约定：参数用基础类型（string/int/float64/map[string]any/[]any），返回单值或 `(result, error)`。

## 内置模块

| 构造器 | 模块名 | 函数面 |
|---|---|---|
| `ModuleLogger(logger)` | `log` | `info` / `warn` / `error` / `debug` 及 `*f` 格式化变体（float64 整数自动转 int，兼容 `%d`） |
| `ModuleCrypto()` | `crypto` | `encrypt` / `decrypt` / `is_encrypted` / `encrypt_json` / `decrypt_json` / `hash_sha256`（底层 [go-utils/crypto](https://github.com/tx7do/go-utils) 全局加密器，`enc:` 前缀按需加密） |
| `ModuleUtil(maxSleep...)` | `util` | `sleep`（默认上限 5s，防脚本无限阻塞执行线程）、`time` / `timestamp` / `date`（默认 RFC3339） |
| `ModuleHTTP(opts)` | `http` | `request(url, options?)` / `get(url)` / `post(url, body, contentType?)` → `{status, body, headers(全小写键)}` |

## HTTP 出站护栏

`ModuleHTTP` 与底层原语（`NormalizeHTTPOptions` / `CheckAllowedURL` / `HTTPRequest` / `BuildHTTPClient`）实现同一套护栏，Lua / JS 两侧共享：

- **fail-closed 白名单**：`AllowedDomains` 为空 = 全部出站拒绝。部署侧经环境变量 `SCRIPT_HTTP_ALLOWED_DOMAINS`（逗号分隔，如 `hooks.slack.com,oapi.dingtalk.com`）放行，`HTTPAllowlistFromEnv()` 一行构造。
- **协议与域名**：仅 http/https；host 需精确命中白名单，`*.example.com` 只通配**一级**子域（`a.b.example.com` 不命中）。
- **SSRF 基线**：`localhost` / `127.0.0.1` / `0.0.0.0` / `::1` / `169.254.169.254`（云元数据地址）硬禁——即使白名单显式列出也拒绝；`DenyLoopback=false` 仅为 httptest 等测试环境留的逃生门。
- **重定向逐跳复检**：`CheckRedirect` 内对每个跳转目标重新执行白名单校验，上限 3 跳——白名单域名 302 到内网同样被拦。
- **体积/超时上限**：单请求超时默认 10s（配置 >30s 收敛回 10s）；请求体 / 响应体上限各 1MB，超限即错不截断。
- **防护头不可伪装**：脚本传入的 `host` / `user-agent` / `content-length` 头被丢弃；UA 固定为 `go-scripts-host/1.0`。

## 用法

```go
opts := hostmodule.HTTPAllowlistFromEnv() // 或手工构造 HTTPOptions
mod := hostmodule.ModuleHTTP(opts)        // mod.Name="http", mod.Funcs 注册进你的 VM

logMod := hostmodule.ModuleLogger(logger) // logger 为 kratos-bootstrap/logger Helper
```

```js
// 脚本侧（goja）
const http = require('http'); // 或 VM 全局，取决于宿主注册方式
const resp = http.get('https://hooks.slack.com/services/xxx');
if (resp.status === 200) { /* resp.body / resp.headers */ }
```

## 依赖

- Go 1.25+
- [`github.com/tx7do/go-utils/crypto`](https://github.com/tx7do/go-utils)（crypto 模块）
- [`github.com/tx7do/kratos-bootstrap/logger`](https://github.com/tx7do/kratos-bootstrap)（logger 模块）
