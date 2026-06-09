# Database Source

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://golang.org)

**Universal script source based on the standard database/sql, supporting MySQL / PostgreSQL / SQLite and all SQL databases**

[中文](README.md) · [English](README_EN.md) · [日本語](README_JP.md)

---

## Overview

Database Source uses Go's standard `database/sql` package to read scripts from any SQL database. Hot-reload is detected via checksum column polling. It supports both auto-generated and custom SQL query modes.

### Features

| Feature | Description |
| --- | --- |
| Library | Go standard `database/sql` |
| Hot-Reload | Checksum column comparison polling (default 10s) |
| Databases | MySQL, PostgreSQL, SQLite, SQL Server, and all SQL databases |
| Key Prefix | `WithPrefix` namespace isolation |
| Connection Pool | Configurable max open, max idle, and connection lifetime |
| Custom Query | Full SQL customization via `WithQuery` |
| Interface | Implements `source.ReadWatcher` |

---

## Installation

```bash
go get github.com/tx7do/go-scripts/source/database
```

Install the driver for your database:

```bash
# MySQL
go get github.com/go-sql-driver/mysql

# PostgreSQL (pgx)
go get github.com/jackc/pgx/v5/stdlib

# SQLite
go get modernc.org/sqlite
```

---

## Table Schema

Recommended table structure (fully customizable):

```sql
CREATE TABLE scripts (
    name        VARCHAR(255) PRIMARY KEY,
    content     TEXT          NOT NULL,
    updated_at  TIMESTAMP     DEFAULT CURRENT_TIMESTAMP
);
```

- `name` — Script identifier (Key column)
- `content` — Script content (Value column)
- `updated_at` — Change detection column (Checksum column), changes on every update

---

## Configuration Options

| Option | Default | Description |
| --- | --- | --- |
| `WithDriver(driver)` | Required | Database driver name |
| `WithDSN(dsn)` | Required | Data source name |
| `WithDB(db)` | nil | Inject existing `*sql.DB` (overrides Driver/DSN) |
| `WithTable(table)` | `scripts` | Table name |
| `WithKeyColumn(col)` | `name` | Key column name |
| `WithValueColumn(col)` | `content` | Script content column name |
| `WithChecksumColumn(col)` | `updated_at` | Change detection column name |
| `WithQuery(sql)` | Auto-generated | Custom SQL query |
| `WithPrefix(prefix)` | empty | Key prefix |
| `WithPollInterval(d)` | `10s` | Watch polling interval |
| `WithMaxOpenConns(n)` | Driver default | Max open connections |
| `WithMaxIdleConns(n)` | Driver default | Max idle connections |
| `WithConnMaxLifetime(d)` | Driver default | Connection max lifetime |

---

## Quick Start

### Auto-Generated Query Mode

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

### Custom Query Mode

```go
src, err := dbSrc.New(ctx,
    dbSrc.WithDriver("postgres"),
    dbSrc.WithDSN("host=localhost dbname=scripts"),
    dbSrc.WithQuery("SELECT body, version FROM my_scripts WHERE id = $1"),
)
```

### Shared Connection Pool Mode

```go
import "database/sql"

db, _ := sql.Open("mysql", dsn)

src, err := dbSrc.New(ctx,
    dbSrc.WithDB(db),  // Reader will NOT close db
)
```

### Hot-Reload

```go
// 1. Load initial version
code, _ := src.Load(ctx, "hello.lua")

// 2. Watch for changes
ch, _ := src.Watch(ctx, "hello.lua")
for range ch {
    // 3. Reload
    code, _ = src.Load(ctx, "hello.lua")
    fmt.Println("Script updated")
}
```

---

## Testing

```bash
cd source/database && go test -v ./...
```

---

## Related Documentation

- [Back to Source Documentation](../README_EN.md)
- [Back to Main Documentation](../../README_EN.md)

## License

[MIT License](../../LICENSE)
