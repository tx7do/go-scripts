# S3 Source

[`go-scripts`](../../) 向けに [Amazon S3](https://aws.amazon.com/s3/)（および S3 互換オブジェクトストレージ）をスクリプトソースとして提供する [`Source`](../../source.go) 実装。

## 設計要点

- **統一インターフェース**：[`scriptEngine.Source`](../../source.go#L15-L25) を実装。[`Engine`](../../engine.go) のスクリプトソースとして直接使用可能、または [`MultiSource`](../../source.go#L180-L299) に組み込んで他のソース（File/Mem/HTTP/...）と fallback / 同時実行で最速応答を返す戦略に利用可能。
- **AWS SDK v2**：[`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2) ベース。完全なデフォルトクレデンシャルチェーン（環境変数 / shared config / IMDS / ECS / SSO）をサポート。
- **S3 互換対応**：`WithEndpoint` + `WithPathStyle` で MinIO / Alibaba OSS / Tencent COS / Cloudflare R2 / LocalStack などに接続可能。
- **ホットリロード検出**：ETag（優先）と LastModified（フォールバック）に基づくバージョン比較。FileSource の mtime 処理と同等。
- **並行安全性**：すべてのメソッド（`Load` / `ReloadCheck` / `Close`）は goroutine-safe。バージョン番号の比較は `sync.RWMutex` で保護。
- **テスト容易性**：内部で `s3API` サブセットインターフェースを定義。テストは `httptest.NewServer` で最小限の S3 互換サービスを起動。実際の bucket は不要。

## 依存関係

- Go 1.24+
- [`github.com/aws/aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2)
- [`github.com/aws/aws-sdk-go-v2/config`](https://github.com/aws/aws-sdk-go-v2/tree/main/config)
- [`github.com/aws/aws-sdk-go-v2/credentials`](https://github.com/aws/aws-sdk-go-v2/tree/main/credentials)
- [`github.com/aws/aws-sdk-go-v2/service/s3`](https://github.com/aws/aws-sdk-go-v2/tree/main/service/s3)
- [`github.com/aws/smithy-go`](https://github.com/aws/smithy-go)
- [`github.com/tx7do/go-scripts`](../../) —— ルートモジュール

## クイックスタート

### 1. インポート

```go
import (
    "context"

    scriptEngine "github.com/tx7do/go-scripts"
    _ "github.com/tx7do/go-scripts/lua"   // Lua エンジンファクトリを登録
    s3src "github.com/tx7do/go-scripts/s3"
)
```

### 2. AWS S3（デフォルトクレデンシャルチェーン）

```go
ctx := context.Background()

src, err := s3src.New(ctx, "my-prod-scripts",
    s3src.WithRegion("ap-northeast-1"),
    s3src.WithPrefix("lua/"), // bucket 内の共通プレフィックス
)
if err != nil {
    log.Fatal(err)
}
defer src.Close()

// Engine に接続
eng, _ := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
_ = eng.Init(ctx)
eng.SetSource(src)

// Load は s3://my-prod-scripts/lua/main.lua から取得
_, err = eng.ExecuteFromKey(ctx, "main.lua")
```

クレデンシャルは AWS SDK デフォルトチェーンで解決：
1. `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` + `AWS_SESSION_TOKEN` 環境変数
2. `AWS_PROFILE` が指す shared config（`~/.aws/credentials`）
3. EC2 IMDS / ECS task role / IRSA / SSO

### 3. MinIO / セルフホストオブジェクトストレージ

```go
src, err := s3src.New(ctx, "scripts",
    s3src.WithEndpoint("http://minio.minio.svc.cluster.local:9000"),
    s3src.WithPathStyle(),                              // MinIO に必須
    s3src.WithRegion("us-east-1"),                      // MinIO は気にしないが SDK に必要
    s3src.WithStaticCredentials("AK", "SK", ""),
)
```

### 4. Cloudflare R2

```go
src, err := s3src.New(ctx, "my-bucket",
    s3src.WithEndpoint("https://<ACCOUNT_ID>.r2.cloudflarestorage.com"),
    s3src.WithStaticCredentials(r2AccessKey, r2SecretKey, ""),
    s3src.WithRegion("auto"),   // R2 は任意の region を受け付ける
)
```

### 5. 公開 bucket への匿名アクセス

```go
src, err := s3src.New(ctx, "public-scripts",
    s3src.WithRegion("us-east-1"),
    s3src.WithAnonymous(),  // 署名なし
)
```

## コア API

### コンストラクタ

| 関数 | 説明 |
|---|---|
| `New(ctx, bucket, opts...)` | `*Source` を構築。`bucket` は必須、他はオプション |

### Options

| Option | 役割 | デフォルト |
|---|---|---|
| `WithRegion(region)` | AWS region | デフォルトチェーンで解決 |
| `WithEndpoint(url)` | カスタム endpoint（MinIO/OSS/R2/LocalStack）| AWS デフォルト |
| `WithPathStyle()` | path-style アドレッシングを強制（`endpoint/bucket/key`）| virtual-hosted |
| `WithPrefix(prefix)` | key プレフィックス。先頭の `/` は削除、末尾に `/` を補完 | 無し |
| `WithCredentials(p)` | カスタム `aws.CredentialsProvider` | デフォルトチェーン |
| `WithStaticCredentials(ak, sk, token)` | 静的クレデンシャル（`WithCredentials` をラップ） | — |
| `WithAnonymous()` | 署名を無効化（公開 bucket / テスト fake） | — |

### Source メソッド

| メソッド | 説明 |
|---|---|
| `Load(ctx, key)` | S3 からオブジェクトを GET し、body 文字列を返す。現在の ETag/LastModified をバージョン基準として記録 |
| `ReloadCheck(ctx, key)` | オブジェクトを HEAD し、ETag（優先）または LastModified（フォールバック）を比較 |
| `Close()` | リソースを解放（AWS SDK v2 S3 client に明示的な Close は無く、現在は no-op）|

## エラー処理

| エラー | 説明 |
|---|---|
| `ErrNotFound` | オブジェクトが存在しない（404 / NoSuchKey）。`errors.Is(err, s3src.ErrNotFound)` または `s3src.IsNotFound(err)` で識別 |
| その他 | SDK の元のエラーを `s3 source: ...` プレフィックス付きでラップ。完全な object key を含む |

```go
code, err := src.Load(ctx, key)
if errors.Is(err, s3src.ErrNotFound) {
    // オブジェクトが存在しない
} else if err != nil {
    // ネットワーク / 権限 / その他
}
```

## Engine との統合

### 単一エンジン

```go
eng, _ := scriptEngine.NewScriptEngine(scriptEngine.LuaType)
_ = eng.Init(ctx)
eng.SetSource(src)                  // Source をバインド

_, _ = eng.ExecuteFromKey(ctx, "init.lua")
_, _ = eng.ExecuteFromKey(ctx, "main.lua")
```

### エンジンプール

```go
pool, _ := scriptEngine.NewEnginePool(8, scriptEngine.LuaType)
defer pool.Close()
pool.SetSource(src)                 // 注意：下記参照

_, _ = pool.ExecuteFromKey(ctx, "init.lua")
```

> 注意：`EnginePool.SetSource` は "Acquire された 1 つの engine インスタンス" にのみ作用します。プール内の他の engine は source=nil のままです。すべての engine に同じ Source を使わせたい場合：
> - engine 作成時にあらかじめ SetSource してからプールに追加、または
> - プール内の engine を順次 SetSource、または
> - `pool.ExecuteFromKey(ctx, key)` を直接使用（1 コールごとに一時的に SetSource するが、外部 lock での保護が必要。上 2 つを推奨）。
>
> 現在の pool wrapper の "per-call ローカル状態セマンティクス" については [engine_pool.go のコメント](../../engine_pool.go) を参照。

### MultiSource で fallback

```go
mem := scriptEngine.NewMemSource()
mem.Set("main.lua", `-- fallback inline script`)

multi, _ := scriptEngine.NewFallbackSource(src, mem) // S3 優先、失敗時メモリにフォールバック
eng.SetSource(multi)
```

## ホットリロード検出

`ReloadCheck` のワークフロー：

```
HEAD s3://bucket/prefix/key
  ↓
(ETag, LastModified) を取得
  ↓
Load 時に記録したバージョンと比較
  ↓
差異あり -> changed=true
```

- **ETag 優先**：S3 は PUT したオブジェクトに対しデフォルトで MD5 ベースの ETag を返す（multipart upload のオブジェクトは合成 hash の ETag）。ETag が異なれば内容は確実に変わっている。
- **LastModified フォールバック**：いずれかの側の ETag が空の場合（一部の S3 互換サービスは返さない）、LastModified の比較に退化。HTTP Last-Modified の精度は **1 秒** で、秒未満の連続変更はマージされる可能性がある。
- **未 Load の key**：`changed=true`（FileSource の挙動と一致）。

`ReloadCheck` は「変更通知」だけで、**自動再読み込みはしない**。ビジネス側で明示的に `Load` / `ExecuteFromKey` を再度呼び出す必要がある：

```go
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    if changed, _ := src.ReloadCheck(ctx, "main.lua"); changed {
        _, _ = eng.ExecuteFromKey(ctx, "main.lua") // ホットリロード発動
    }
}
```

## テスト

```bash
cd source/s3
go test -v ./...
```

カバレッジ：

| カテゴリ | ケース |
|---|---|
| インターフェース実装 | `TestSource_ImplementsInterface`（コンパイル時アサーション）|
| 構築 | `TestNew_RequiresBucket` / `TestWithPrefix_Normalized`（6 種類の prefix 書き方）|
| Load | `TestLoad_HappyPath` / `TestLoad_NotFound_WrapsSentinel` / `TestLoad_WithPrefix` / `TestLoad_ContextCanceled` |
| ReloadCheck | `TestReloadCheck_NewKey_IsChanged` / `TestReloadCheck_AfterLoad_NotChanged` / `TestReloadCheck_AfterMutation_IsChanged` / `TestReloadCheck_NotFound` / `TestReloadCheck_FallsBackToLastModified` |
| クレデンシャル | `TestNew_WithStaticCredentials` |
| 並行処理 | `TestLoad_Concurrent`（30 goroutine で並行 Load + カウント検証）|
| リソース | `TestClose_NoError` |

テストは `httptest.NewServer` で最小限の S3 互換サービス（path-style、署名無視、CRC32C checksum を返す）を起動し、実際の S3 bucket に依存しない CI フレンドリーな構成。

## 関連ドキュメント

- ルートモジュール README：[../../README_JP.md](../../README_JP.md)
- Source インターフェース定義：[../../source.go](../../source.go)
- FileSource / MemSource / MultiSource：[../../source.go](../../source.go)
- AWS SDK for Go v2：<https://github.com/aws/aws-sdk-go-v2>
- AWS S3 API Reference：<https://docs.aws.amazon.com/AmazonS3/latest/API/>
- Cloudflare R2 S3 互換性：<https://developers.cloudflare.com/r2/api/s3/api/>
- MinIO S3 互換 API：<https://min.io/docs/minio/linux/developers/go/API.html>
