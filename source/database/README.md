# Database Source · 数据库脚本来源

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**基于标准 database/sql 的通用脚本来源，支持 MySQL / PostgreSQL / SQLite 等所有 SQL 数据库**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## 概述

Database Source 使用 Go 标准库 `database/sql` 从任意 SQL 数据库读取脚本。通过校验和列比对轮询检测热更新。支持自动生成查询和自定义 SQL 两种模式。

### 特性

| 特性 | 说明 |
| --- | --- |
| 底层库 | Go 标准库 `database/sql` |
| 热更新 | 校验和列比对轮询（默认 10s） |
| 数据库 | MySQL、PostgreSQL、SQLite、SQL Server 等所有 SQL 数据库 |
| Key 前缀 | 支持 `WithPrefix` 命名空间隔离 |
| 连接池 | 支持自定义最大连接数、空闲连接数、连接生命周期 |
| 自定义查询 | 支持 `WithQuery` 完全自定义 SQL |
| 接口 | 实现 `source.ReadWatcher` |

---

## 安装

```bash
go get github.com/tx7do/go-scripts/source/database
```

需要同时安装对应数据库的驱动，例如：

```bash
# MySQL
go get github.com/go-sql-driver/mysql

# PostgreSQL (pgx)
go get github.com/jackc/pgx/v5/stdlib

# SQLite
go get modernc.org/sqlite
```

---

## 数据库表结构

推荐的表结构（可自定义）：

```sql
CREATE TABLE scripts (
    name        VARCHAR(255) PRIMARY KEY,
    content     TEXT          NOT NULL,
    updated_at  TIMESTAMP     DEFAULT CURRENT_TIMESTAMP
);
```

- `name` — 脚本标识符（Key 列）
- `content` — 脚本内容（Value 列）
- `updated_at` — 变更检测列（Checksum 列），每次更新自动变化

---

## 配置选项

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `WithDriver(driver)` | 必填 | 数据库驱动名称 |
| `WithDSN(dsn)` | 必填 | 数据源名称 |
| `WithDB(db)` | 空 | 注入已有 `*sql.DB`（优先于 Driver/DSN） |
| `WithTable(table)` | `scripts` | 表名 |
| `WithKeyColumn(col)` | `name` | Key 列名 |
| `WithValueColumn(col)` | `content` | 脚本内容列名 |
| `WithChecksumColumn(col)` | `updated_at` | 变更检测列名 |
| `WithQuery(sql)` | 自动生成 | 自定义 SQL 查询 |
| `WithPrefix(prefix)` | 空 | Key 前缀 |
| `WithPollInterval(d)` | `10s` | Watch 轮询间隔 |
| `WithMaxOpenConns(n)` | 驱动默认 | 最大打开连接数 |
| `WithMaxIdleConns(n)` | 驱动默认 | 最大空闲连接数 |
| `WithConnMaxLifetime(d)` | 驱动默认 | 连接最大生命周期 |

---

## 快速开始

### 自动生成查询模式

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

### 自定义查询模式

```go
src, err := dbSrc.New(ctx,
    dbSrc.WithDriver("postgres"),
    dbSrc.WithDSN("host=localhost dbname=scripts"),
    dbSrc.WithQuery("SELECT body, version FROM my_scripts WHERE id = $1"),
)
```

### 共享连接池模式

```go
import "database/sql"

db, _ := sql.Open("mysql", dsn)

src, err := dbSrc.New(ctx,
    dbSrc.WithDB(db),  // Reader 不会关闭 db
)
```

### 热更新

```go
// 1. 先 Load 获取初始版本
code, _ := src.Load(ctx, "hello.lua")

// 2. Watch 监听变更
ch, _ := src.Watch(ctx, "hello.lua")
for range ch {
    // 3. 重新加载
    code, _ = src.Load(ctx, "hello.lua")
    fmt.Println("脚本已更新")
}
```

---

## 测试

```bash
cd source/database && go test -v ./...
```

---

## 相关文档

- [返回 Source 文档](../README.md)
- [返回主文档](../../README.md)

## License

[MIT License](../../LICENSE)
