# S3 Source

为 [`go-scripts`](../../) 提供 [Amazon S3](https://aws.amazon.com/s3/)（以及任何 S3 兼容的对象存储）作为脚本来源的 [`Source`](../../source.go) 实现。

## 设计要点

- **统一接口**：实现 [`scriptEngine.Source`](../../source.go#L15-L25)，可作为 [`Engine`](../../engine.go) 的脚本来源，或拼入 [`MultiSource`](../../source.go#L180-L299) 与其他源（File/Mem/HTTP/...）做 fallback / 并发择快。
- **AWS SDK v2**：基于 [`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2)，支持完整的默认凭证链（环境变量 / shared config / IMDS / ECS / SSO）。
- **S3-compatible 友好**：通过 `WithEndpoint` + `WithPathStyle` 即可接入 MinIO / Alibaba OSS / Tencent COS / Cloudflare R2 / LocalStack 等。
- **热更新检测**：基于 ETag（首选）和 LastModified（兜底）的版本比对，等价于 FileSource 对 mtime 的处理。
- **并发安全**：所有方法（`Load` / `ReloadCheck` / `Close`）都是 goroutine-safe；版本号比对用 `sync.RWMutex` 保护。
- **可测试**：内部定义 `s3API` 子集接口，测试用 `httptest.NewServer` 起一个最小 S3 兼容服务即可覆盖；不需要真实 bucket。

## 依赖

- Go 1.24+
- [`github.com/aws/aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2)
- [`github.com/aws/aws-sdk-go-v2/config`](https://github.com/aws/aws-sdk-go-v2/tree/main/config)
- [`github.com/aws/aws-sdk-go-v2/credentials`](https://github.com/aws/aws-sdk-go-v2/tree/main/credentials)
- [`github.com/aws/aws-sdk-go-v2/service/s3`](https://github.com/aws/aws-sdk-go-v2/tree/main/service/s3)
- [`github.com/aws/smithy-go`](https://github.com/aws/smithy-go)
- [`github.com/tx7do/go-scripts`](../../) —— 根模块

## 快速开始

### 1. 引入模块

```go
import (
    "context"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua"   // 注册 Lua 引擎工厂
    s3src "github.com/tx7do/go-scripts/s3"
)
```

### 2. AWS S3（默认凭证链）

```go
ctx := context.Background()

src, err := s3src.New(ctx, "my-prod-scripts",
    s3src.WithRegion("ap-northeast-1"),
    s3src.WithPrefix("lua/"), // bucket 内公共前缀
)
if err != nil {
    log.Fatal(err)
}
defer src.Close()

// 接入 Engine
eng, _ := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
_ = eng.Init(ctx)
eng.SetSource(src)

// Load 实际是从 s3://my-prod-scripts/lua/main.lua 拉取
_, err = eng.ExecuteFromKey(ctx, "main.lua")
```

凭证按 AWS SDK 默认链解析：
1. `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` + `AWS_SESSION_TOKEN` 环境变量
2. `AWS_PROFILE` 指向的 shared config（`~/.aws/credentials`）
3. EC2 IMDS / ECS task role / IRSA / SSO

### 3. MinIO / 自建对象存储

```go
src, err := s3src.New(ctx, "scripts",
    s3src.WithEndpoint("http://minio.minio.svc.cluster.local:9000"),
    s3src.WithPathStyle(),                              // MinIO 必需
    s3src.WithRegion("us-east-1"),                      // MinIO 不关心，但 SDK 需要
    s3src.WithStaticCredentials("AK", "SK", ""),
)
```

### 4. Cloudflare R2

```go
src, err := s3src.New(ctx, "my-bucket",
    s3src.WithEndpoint("https://<ACCOUNT_ID>.r2.cloudflarestorage.com"),
    s3src.WithStaticCredentials(r2AccessKey, r2SecretKey, ""),
    s3src.WithRegion("auto"),   // R2 接受任意 region
)
```

### 5. 匿名访问公共 bucket

```go
src, err := s3src.New(ctx, "public-scripts",
    s3src.WithRegion("us-east-1"),
    s3src.WithAnonymous(),  // 不带签名
)
```

## 核心 API

### 构造

| 函数 | 说明 |
|---|---|
| `New(ctx, bucket, opts...)` | 构造 `*Source`。`bucket` 必填，其它都是可选 Option |

### Options

| Option | 作用 | 默认 |
|---|---|---|
| `WithRegion(region)` | AWS region | 由默认链解析 |
| `WithEndpoint(url)` | 自定义 endpoint（MinIO/OSS/R2/LocalStack）| AWS 默认 |
| `WithPathStyle()` | 强制 path-style 寻址（`endpoint/bucket/key`）| virtual-hosted |
| `WithPrefix(prefix)` | key 前缀，自动去除首 `/` 并补尾 `/` | 无 |
| `WithCredentials(p)` | 自定义 `aws.CredentialsProvider` | 默认链 |
| `WithStaticCredentials(ak, sk, token)` | 静态凭证（封装 `WithCredentials`） | — |
| `WithAnonymous()` | 关闭签名（公共 bucket / 测试 fake） | — |

### Source 方法

| 方法 | 说明 |
|---|---|
| `Load(ctx, key)` | 从 S3 GET 对象，返回 body 字符串；记录当前 ETag/LastModified 作为版本基准 |
| `ReloadCheck(ctx, key)` | HEAD 对象，比对 ETag（首选）或 LastModified（兜底） |
| `Close()` | 释放资源（AWS SDK v2 S3 client 无显式 Close，目前为 no-op）|

## 错误处理

| 错误 | 说明 |
|---|---|
| `ErrNotFound` | 对象不存在（404 / NoSuchKey）；用 `errors.Is(err, s3src.ErrNotFound)` 或 `s3src.IsNotFound(err)` 识别 |
| 其它 | 包装 SDK 原始错误，前缀为 `s3 source: ...`，包含完整 object key |

```go
code, err := src.Load(ctx, key)
if errors.Is(err, s3src.ErrNotFound) {
    // 对象不存在
} else if err != nil {
    // 网络 / 权限 / 其它
}
```

## 与 Engine 集成

### 单引擎

```go
eng, _ := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
_ = eng.Init(ctx)
eng.SetSource(src)                  // 绑定 Source

_, _ = eng.ExecuteFromKey(ctx, "init.lua")
_, _ = eng.ExecuteFromKey(ctx, "main.lua")
```

### 引擎池

```go
pool, _ := scriptEngine.NewEnginePool(8, scriptEngine.LuaType)
defer pool.Close()
pool.SetSource(src)                 // ⚠️ 警告：见下

_, _ = pool.ExecuteFromKey(ctx, "init.lua")
```

> ⚠️ **重要警告**：`EnginePool.SetSource` 只作用于"被 Acquire 的那一个 engine 实例"。池中其它 engine 仍然 source=nil。如果想让所有 engine 都用同一个 Source，请：
> - 在创建 engine 时预先 SetSource，再加入池；或
> - 自己遍历池内的 engine 逐个 SetSource；或
> - 直接用 `pool.ExecuteFromKey(ctx, key)`，让每次调用都临时 SetSource（需要外层 lock 保护，更推荐前两种方案）。
>
> 当前 pool wrapper 的"per-call 局部状态语义"详见 [engine_pool.go 注释](../../engine_pool.go)。

### 配合 MultiSource 做 fallback

```go
mem := scriptEngine.NewMemSource()
mem.Set("main.lua", `-- fallback inline script`)

multi, _ := scriptEngine.NewFallbackSource(src, mem) // S3 优先，失败回退到内存
eng.SetSource(multi)
```

## 热更新检测

`ReloadCheck` 的工作流：

```
HEAD s3://bucket/prefix/key
  ↓
拿到 (ETag, LastModified)
  ↓
与 Load 时记录的版本比对
  ↓
不同 → changed=true
```

- **首选 ETag**：S3 对 PUT 的对象默认返回一个 MD5-based ETag（multipart upload 的对象 ETag 是合成 hash）。ETag 不同 → 内容一定变了。
- **兜底 LastModified**：当任一侧 ETag 为空（某些 S3 兼容服务不返回），退化到 LastModified 比对。注意 HTTP Last-Modified 精度为 **1 秒**，秒级内的连续修改可能被合并。
- **未 Load 过的 key**：`changed=true`（与 FileSource 行为一致）。

`ReloadCheck` 只是"通知有变化"，**不会自动重新加载**。需要业务侧显式再调一次 `Load` / `ExecuteFromKey`：

```go
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    if changed, _ := src.ReloadCheck(ctx, "main.lua"); changed {
        _, _ = eng.ExecuteFromKey(ctx, "main.lua") // 热更新生效
    }
}
```

## 测试

```bash
cd source/s3
go test -v ./...
```

测试覆盖：

| 类别 | 用例 |
|---|---|
| 接口实现 | `TestSource_ImplementsInterface`（编译期断言）|
| 构造 | `TestNew_RequiresBucket` / `TestWithPrefix_Normalized`（6 种 prefix 写法）|
| Load | `TestLoad_HappyPath` / `TestLoad_NotFound_WrapsSentinel` / `TestLoad_WithPrefix` / `TestLoad_ContextCanceled` |
| ReloadCheck | `TestReloadCheck_NewKey_IsChanged` / `TestReloadCheck_AfterLoad_NotChanged` / `TestReloadCheck_AfterMutation_IsChanged` / `TestReloadCheck_NotFound` / `TestReloadCheck_FallsBackToLastModified` |
| 凭证 | `TestNew_WithStaticCredentials` |
| 并发 | `TestLoad_Concurrent`（30 goroutine 并发 Load + 计数校验）|
| 资源 | `TestClose_NoError` |

测试通过 `httptest.NewServer` 起一个最小 S3 兼容服务（path-style、忽略签名、返回 CRC32C checksum），不依赖任何真实 S3 bucket，CI 友好。

## 相关文档

- 根模块 README：[../../README.md](../../README.md)
- Source 接口定义：[../../source.go](../../source.go)
- FileSource / MemSource / MultiSource：[../../source.go](../../source.go)
- AWS SDK for Go v2：<https://github.com/aws/aws-sdk-go-v2>
- AWS S3 API Reference：<https://docs.aws.amazon.com/AmazonS3/latest/API/>
- Cloudflare R2 S3 兼容性：<https://developers.cloudflare.com/r2/api/s3/api/>
- MinIO S3 兼容 API：<https://min.io/docs/minio/linux/developers/go/API.html>
