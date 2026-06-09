# S3 Source

An [`Source`](../../source.go) implementation that provides [Amazon S3](https://aws.amazon.com/s3/) (and any S3-compatible object storage) as a script source for [`go-scripts`](../../).

## Design Highlights

- **Unified Interface**: Implements [`scriptEngine.Source`](../../source.go#L15-L25). Can be used as the script source for [`Engine`](../../engine.go), or composed into [`MultiSource`](../../source.go#L180-L299) with other sources (File/Mem/HTTP/...) for fallback / concurrent fastest-wins strategies.
- **AWS SDK v2**: Built on [`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2), supporting the full default credential chain (environment variables / shared config / IMDS / ECS / SSO).
- **S3-compatible Friendly**: Connect to MinIO / Alibaba OSS / Tencent COS / Cloudflare R2 / LocalStack via `WithEndpoint` + `WithPathStyle`.
- **Hot-Reload Detection**: Version comparison based on ETag (preferred) and LastModified (fallback), equivalent to FileSource's mtime handling.
- **Concurrency Safe**: All methods (`Load` / `ReloadCheck` / `Close`) are goroutine-safe; version comparison is guarded by `sync.RWMutex`.
- **Testable**: Internally defines an `s3API` subset interface; tests use `httptest.NewServer` to start a minimal S3-compatible service. No real bucket needed.

## Dependencies

- Go 1.24+
- [`github.com/aws/aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2)
- [`github.com/aws/aws-sdk-go-v2/config`](https://github.com/aws/aws-sdk-go-v2/tree/main/config)
- [`github.com/aws/aws-sdk-go-v2/credentials`](https://github.com/aws/aws-sdk-go-v2/tree/main/credentials)
- [`github.com/aws/aws-sdk-go-v2/service/s3`](https://github.com/aws/aws-sdk-go-v2/tree/main/service/s3)
- [`github.com/aws/smithy-go`](https://github.com/aws/smithy-go)
- [`github.com/tx7do/go-scripts`](../../) — root module

## Quick Start

### 1. Import

```go
import (
    "context"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua"   // Register Lua engine factory
    s3src "github.com/tx7do/go-scripts/s3"
)
```

### 2. AWS S3 (Default Credential Chain)

```go
ctx := context.Background()

src, err := s3src.New(ctx, "my-prod-scripts",
    s3src.WithRegion("ap-northeast-1"),
    s3src.WithPrefix("lua/"), // Common prefix inside the bucket
)
if err != nil {
    log.Fatal(err)
}
defer src.Close()

// Plug into Engine
eng, _ := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
_ = eng.Init(ctx)
eng.SetSource(src)

// Load actually fetches from s3://my-prod-scripts/lua/main.lua
_, err = eng.ExecuteFromKey(ctx, "main.lua")
```

Credentials are resolved according to the AWS SDK default chain:
1. `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` + `AWS_SESSION_TOKEN` environment variables
2. Shared config pointed to by `AWS_PROFILE` (`~/.aws/credentials`)
3. EC2 IMDS / ECS task role / IRSA / SSO

### 3. MinIO / Self-hosted Object Storage

```go
src, err := s3src.New(ctx, "scripts",
    s3src.WithEndpoint("http://minio.minio.svc.cluster.local:9000"),
    s3src.WithPathStyle(),                              // Required by MinIO
    s3src.WithRegion("us-east-1"),                      // MinIO doesn't care, but SDK requires it
    s3src.WithStaticCredentials("AK", "SK", ""),
)
```

### 4. Cloudflare R2

```go
src, err := s3src.New(ctx, "my-bucket",
    s3src.WithEndpoint("https://<ACCOUNT_ID>.r2.cloudflarestorage.com"),
    s3src.WithStaticCredentials(r2AccessKey, r2SecretKey, ""),
    s3src.WithRegion("auto"),   // R2 accepts any region
)
```

### 5. Anonymous Access to a Public Bucket

```go
src, err := s3src.New(ctx, "public-scripts",
    s3src.WithRegion("us-east-1"),
    s3src.WithAnonymous(),  // No signing
)
```

## Core API

### Constructor

| Function | Description |
|---|---|
| `New(ctx, bucket, opts...)` | Construct a `*Source`. `bucket` is required; all others are optional Options |

### Options

| Option | Purpose | Default |
|---|---|---|
| `WithRegion(region)` | AWS region | Resolved by default chain |
| `WithEndpoint(url)` | Custom endpoint (MinIO/OSS/R2/LocalStack) | AWS default |
| `WithPathStyle()` | Force path-style addressing (`endpoint/bucket/key`) | virtual-hosted |
| `WithPrefix(prefix)` | Key prefix; leading `/` stripped, trailing `/` appended | None |
| `WithCredentials(p)` | Custom `aws.CredentialsProvider` | Default chain |
| `WithStaticCredentials(ak, sk, token)` | Static credentials (wraps `WithCredentials`) | — |
| `WithAnonymous()` | Disable signing (public bucket / test fake) | — |

### Source Methods

| Method | Description |
|---|---|
| `Load(ctx, key)` | GET the object from S3, returning the body as a string; records the current ETag/LastModified as the version baseline |
| `ReloadCheck(ctx, key)` | HEAD the object, comparing ETag (preferred) or LastModified (fallback) |
| `Close()` | Release resources (AWS SDK v2 S3 client has no explicit Close; currently a no-op) |

## Error Handling

| Error | Description |
|---|---|
| `ErrNotFound` | Object does not exist (404 / NoSuchKey); identify with `errors.Is(err, s3src.ErrNotFound)` or `s3src.IsNotFound(err)` |
| Others | Wraps the original SDK error with prefix `s3 source: ...`, including the full object key |

```go
code, err := src.Load(ctx, key)
if errors.Is(err, s3src.ErrNotFound) {
    // Object does not exist
} else if err != nil {
    // Network / permissions / others
}
```

## Engine Integration

### Single Engine

```go
eng, _ := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
_ = eng.Init(ctx)
eng.SetSource(src)                  // Bind Source

_, _ = eng.ExecuteFromKey(ctx, "init.lua")
_, _ = eng.ExecuteFromKey(ctx, "main.lua")
```

### Engine Pool

```go
pool, _ := scriptEngine.NewEnginePool(8, scriptEngine.LuaType)
defer pool.Close()
pool.SetSource(src)                 // Warning: see below

_, _ = pool.ExecuteFromKey(ctx, "init.lua")
```

> Warning: `EnginePool.SetSource` only affects "the one engine instance that was Acquired". Other engines in the pool still have source=nil. If you want all engines to use the same Source:
> - Pre-SetSource at engine creation time before adding to the pool; or
> - Iterate over engines in the pool and SetSource each one; or
> - Use `pool.ExecuteFromKey(ctx, key)` directly, which temporarily SetSource per call (requires external lock protection; first two options are recommended).
>
> See [engine_pool.go comments](../../engine_pool.go) for the "per-call local state semantics" of the current pool wrapper.

### MultiSource Fallback

```go
mem := scriptEngine.NewMemSource()
mem.Set("main.lua", `-- fallback inline script`)

multi, _ := scriptEngine.NewFallbackSource(src, mem) // S3 first, fall back to memory on failure
eng.SetSource(multi)
```

## Hot-Reload Detection

`ReloadCheck` workflow:

```
HEAD s3://bucket/prefix/key
  ↓
Get (ETag, LastModified)
  ↓
Compare with version recorded during Load
  ↓
Different -> changed=true
```

- **ETag preferred**: S3 returns an MD5-based ETag for PUT objects by default (multipart upload objects have a composite hash ETag). Different ETag means content has definitely changed.
- **LastModified fallback**: When ETag is empty on either side (some S3-compatible services don't return it), falls back to LastModified comparison. Note HTTP Last-Modified precision is **1 second**; sub-second consecutive changes may be coalesced.
- **Key that has never been Load-ed**: `changed=true` (consistent with FileSource behavior).

`ReloadCheck` only "notifies of changes" and **does not auto-reload**. The business side must explicitly call `Load` / `ExecuteFromKey` again:

```go
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    if changed, _ := src.ReloadCheck(ctx, "main.lua"); changed {
        _, _ = eng.ExecuteFromKey(ctx, "main.lua") // Hot-reload takes effect
    }
}
```

## Testing

```bash
cd source/s3
go test -v ./...
```

Coverage:

| Category | Cases |
|---|---|
| Interface implementation | `TestSource_ImplementsInterface` (compile-time assertion) |
| Construction | `TestNew_RequiresBucket` / `TestWithPrefix_Normalized` (6 prefix variations) |
| Load | `TestLoad_HappyPath` / `TestLoad_NotFound_WrapsSentinel` / `TestLoad_WithPrefix` / `TestLoad_ContextCanceled` |
| ReloadCheck | `TestReloadCheck_NewKey_IsChanged` / `TestReloadCheck_AfterLoad_NotChanged` / `TestReloadCheck_AfterMutation_IsChanged` / `TestReloadCheck_NotFound` / `TestReloadCheck_FallsBackToLastModified` |
| Credentials | `TestNew_WithStaticCredentials` |
| Concurrency | `TestLoad_Concurrent` (30 goroutines concurrent Load + count check) |
| Resources | `TestClose_NoError` |

Tests use `httptest.NewServer` to start a minimal S3-compatible service (path-style, ignores signatures, returns CRC32C checksum) with no dependency on any real S3 bucket — CI friendly.

## Related Documentation

- Root module README: [../../README_EN.md](../../README_EN.md)
- Source interface definition: [../../source.go](../../source.go)
- FileSource / MemSource / MultiSource: [../../source.go](../../source.go)
- AWS SDK for Go v2: <https://github.com/aws/aws-sdk-go-v2>
- AWS S3 API Reference: <https://docs.aws.amazon.com/AmazonS3/latest/API/>
- Cloudflare R2 S3 compatibility: <https://developers.cloudflare.com/r2/api/s3/api/>
- MinIO S3 compatible API: <https://min.io/docs/minio/linux/developers/go/API.html>
