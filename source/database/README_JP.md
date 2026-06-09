# Database Source · データベーススクリプトソース

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**標準 database/sql ベースの汎用スクリプトソース。MySQL / PostgreSQL / SQLite など全 SQL データベースに対応**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概要

Database Source は Go 標準ライブラリ `database/sql` を使用して、任意の SQL データベースからスクリプトを読み取ります。チェックサム列の比較ポーリングによるホットリロード検出をサポートします。自動生成クエリとカスタム SQL の両方のモードに対応しています。

### 特徴

| 特徴 | 説明 |
| --- | --- |
| ライブラリ | Go 標準 `database/sql` |
| ホットリロード | チェックサム列比較ポーリング（デフォルト 10s） |
| データベース | MySQL、PostgreSQL、SQLite、SQL Server など全 SQL データベース |
| キープレフィックス | `WithPrefix` による名前空間分離 |
| コネクションプール | 最大接続数、アイドル接続数、接続ライフタイムをカスタマイズ可能 |
| カスタムクエリ | `WithQuery` による SQL の完全カスタマイズ |
| インターフェース | `source.ReadWatcher` を実装 |

---

## インストール

```bash
go get github.com/tx7do/go-scripts/source/database
```

使用するデータベースのドライバをインストールします：

```bash
# MySQL
go get github.com/go-sql-driver/mysql

# PostgreSQL (pgx)
go get github.com/jackc/pgx/v5/stdlib

# SQLite
go get modernc.org/sqlite
```

---

## テーブルスキーマ

推奨テーブル構造（カスタマイズ可能）：

```sql
CREATE TABLE scripts (
    name        VARCHAR(255) PRIMARY KEY,
    content     TEXT          NOT NULL,
    updated_at  TIMESTAMP     DEFAULT CURRENT_TIMESTAMP
);
```

- `name` — スクリプト識別子（Key 列）
- `content` — スクリプト内容（Value 列）
- `updated_at` — 変更検出列（Checksum 列）、更新のたびに自動的に変化

---

## 設定オプション

| オプション | デフォルト | 説明 |
| --- | --- | --- |
| `WithDriver(driver)` | 必須 | データベースドライバ名 |
| `WithDSN(dsn)` | 必須 | データソース名 |
| `WithDB(db)` | なし | 既存の `*sql.DB` を注入（Driver/DSN より優先） |
| `WithTable(table)` | `scripts` | テーブル名 |
| `WithKeyColumn(col)` | `name` | Key 列名 |
| `WithValueColumn(col)` | `content` | スクリプト内容の列名 |
| `WithChecksumColumn(col)` | `updated_at` | 変更検出列名 |
| `WithQuery(sql)` | 自動生成 | カスタム SQL クエリ |
| `WithPrefix(prefix)` | 空 | キープレフィックス |
| `WithPollInterval(d)` | `10s` | Watch ポーリング間隔 |
| `WithMaxOpenConns(n)` | ドライバデフォルト | 最大オープン接続数 |
| `WithMaxIdleConns(n)` | ドライバデフォルト | 最大アイドル接続数 |
| `WithConnMaxLifetime(d)` | ドライバデフォルト | 接続最大ライフタイム |

---

## クイックスタート

### 自動生成クエリモード

```go
package main

import (
    "context"
    "fmt"
    _ "github.com/go-sql-driver/mysql"
    dbSrc "github.com/tx7do/go-scripts/source/database"
)

func main() {
    ctx := context.Background()

    src, err := dbSrc.New(ctx,
        dbSrc.WithDriver("mysql"),
        dbSrc.WithDSN("user:pass@tcp(localhost:3306)/scripts"),
        dbSrc.WithTable("scripts"),
        dbSrc.WithKeyColumn("name"),
        dbSrc.WithValueColumn("content"),
        dbSrc.WithChecksumColumn("updated_at"),
    )
    if err != nil { panic(err) }
    defer src.Close()

    code, err := src.Load(ctx, "hello.lua")
    if err != nil { panic(err) }
    fmt.Println(code)
}
```

### カスタムクエリモード

```go
src, err := dbSrc.New(ctx,
    dbSrc.WithDriver("postgres"),
    dbSrc.WithDSN("host=localhost dbname=scripts"),
    dbSrc.WithQuery("SELECT body, version FROM my_scripts WHERE id = $1"),
)
```

### 共有コネクションプールモード

```go
import "database/sql"

db, _ := sql.Open("mysql", dsn)

src, err := dbSrc.New(ctx,
    dbSrc.WithDB(db),  // Reader は db をクローズしません
)
```

### ホットリロード

```go
// 1. 初期バージョンを Load
code, _ := src.Load(ctx, "hello.lua")

// 2. 変更を監視
ch, _ := src.Watch(ctx, "hello.lua")
for range ch {
    // 3. 再読み込み
    code, _ = src.Load(ctx, "hello.lua")
    fmt.Println("スクリプトが更新されました")
}
```

---

## テスト

```bash
cd source/database && go test -v ./...
```

---

## 関連ドキュメント

- [Source ドキュメントに戻る](../README_JP.md)
- [メインドキュメントに戻る](../../README_JP.md)

## ライセンス

[MIT License](../../LICENSE)
