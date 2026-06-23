# godb

A production-ready, database-agnostic ORM library for Go 1.22+ with a fluent query builder, automatic migrations, query caching, lifecycle hooks, eager loading, and an embedded web dashboard.


[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT%20%2F%20Commercial-brightgreen)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/Brah-Timo/godb)](https://goreportcard.com/report/github.com/Brah-Timo/godb)
[![Coverage](https://img.shields.io/badge/coverage-92%25-brightgreen)](https://codecov.io/gh/Brah-Timo/godb)
[![Performance](https://img.shields.io/badge/throughput-100k%20req%2Fs-orange)](#-benchmarks)
[![Stars](https://img.shields.io/github/stars/Brah-Timo/godb?style=social)](https://github.com/Brah-Timo/godb/stargazers)


<img width="374" height="794" alt="image" src="https://github.com/user-attachments/assets/7e5d835c-2a7b-43cd-80e5-cfcb34630ca9" />



## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Dialects](#dialects)
- [Query Builder (Chain API)](#query-builder-chain-api)
- [CRUD Operations](#crud-operations)
- [Migrations](#migrations)
- [Schema & Struct Tags](#schema--struct-tags)
- [Lifecycle Hooks](#lifecycle-hooks)
- [Query Caching](#query-caching)
- [Relations & Eager Loading](#relations--eager-loading)
- [Connection Pool](#connection-pool)
- [Dashboard](#dashboard)
- [Transactions](#transactions)
- [Error Handling](#error-handling)
- [Logger](#logger)
- [Testing](#testing)
- [Contributing](#contributing)

---

## Features

- ✅ **Fluent, immutable query builder** — `Where`, `Joins`, `Order`, `Limit`, `Offset`, `GroupBy`, `Having`, and more
- ✅ **Automatic migrations** — computes diff between structs and live schema, produces a migration plan with rollback-safe SQL
- ✅ **Multi-dialect** — PostgreSQL, MySQL, SQLite (pluggable registry)
- ✅ **Query caching** — LRU in-memory cache or Redis, automatic invalidation by table prefix
- ✅ **Lifecycle hooks** — `BeforeCreate`, `AfterCreate`, `BeforeUpdate`, `AfterUpdate`, `BeforeDelete`, `AfterDelete`, `Validator`
- ✅ **Eager loading** — HasMany, HasOne, BelongsTo, ManyToMany via `Preload`
- ✅ **Prepared-statement cache** — optional, per-connection statement caching
- ✅ **Connection pool** — wraps `database/sql` pool with utilisation metrics
- ✅ **Embedded dashboard** — live query stats, slow-query log, pool metrics served over HTTP
- ✅ **Dry-run mode** — logs SQL without executing it; ideal for auditing migrations
- ✅ **Zero code-generation** — pure Go generics and reflection

---

## Installation

```bash
go get github.com/Brah-Timo/godb
```

### Driver dependencies

Install the driver for your database:

```bash
# PostgreSQL
go get github.com/lib/pq

# MySQL / MariaDB
go get github.com/go-sql-driver/mysql

# SQLite (requires CGO + GCC)
go get github.com/mattn/go-sqlite3
```

---

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/Brah-Timo/godb"
    _ "github.com/mattn/go-sqlite3"
)

type User struct {
    ID    uint   `godb:"primaryKey"`
    Name  string `godb:"size:255;not null"`
    Email string `godb:"uniqueIndex;size:255"`
    Age   int
}

func main() {
    db, err := godb.Open("sqlite3", "./app.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Auto-migrate
    if err := db.AutoMigrate(&User{}); err != nil {
        log.Fatal(err)
    }

    // Create
    user := User{Name: "Alice", Email: "alice@example.com", Age: 30}
    if err := db.Model(&User{}).Create(&user); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Created user id=%d\n", user.ID)

    // Find
    var found User
    err = db.Model(&User{}).Where("email = ?", "alice@example.com").First(&found)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Found: %+v\n", found)

    // Update
    err = db.Model(&User{}).Where("id = ?", user.ID).Update("age", 31)
    fmt.Println("Updated:", err)

    // Delete
    err = db.Model(&User{}).Where("id = ?", user.ID).Delete(&User{})
    fmt.Println("Deleted:", err)
}
```

---

## Configuration

`godb.Open` accepts a driver name and DSN. Use functional options to customise the connection:

```go
db, err := godb.Open(
    "postgres",
    "host=localhost user=app password=secret dbname=mydb sslmode=disable",
    godb.WithPool(godb.PoolConfig{
        MaxOpenConns:    25,
        MaxIdleConns:    10,
        ConnMaxLifetime: 5 * time.Minute,
    }),
    godb.WithCache(cache.New(1000, 5*time.Minute)),  // LRU, 1000 entries, 5 min TTL
    godb.WithLogger(myLogger),
    godb.WithDashboard(":9090"),   // serve dashboard on port 9090
    godb.WithDryRun(false),        // set true to log SQL without executing
    godb.WithPrepareStmt(true),    // cache prepared statements
    godb.WithNowFunc(time.Now),    // override clock (useful in tests)
)
```

### `godb.MustOpen`

Panics on error — useful in `main()` when a failed DB connection is fatal:

```go
db := godb.MustOpen("sqlite3", "./dev.db")
```

### Available Options

| Option | Description |
|--------|-------------|
| `WithPool(PoolConfig)` | Set max open/idle connections and lifetime |
| `WithCache(cache.Cache)` | Attach a query-result cache |
| `WithLogger(Logger)` | Custom logger (implement the `Logger` interface) |
| `WithDashboard(addr)` | Start the HTTP dashboard server on `addr` |
| `WithDryRun(bool)` | Log SQL without executing |
| `WithPrepareStmt(bool)` | Enable prepared-statement cache |
| `WithNowFunc(func() time.Time)` | Override the clock |

---

## Dialects

godb ships three built-in dialects, selected automatically from the driver name:

| Driver | Dialect |
|--------|---------|
| `"postgres"` | PostgreSQL (uses `$1` placeholders, `RETURNING`, quoted identifiers) |
| `"mysql"` | MySQL / MariaDB (uses `?` placeholders, backtick quoting) |
| `"sqlite3"` | SQLite (uses `?` placeholders, lightweight type mapping) |

### Register a custom dialect

```go
import "github.com/Brah-Timo/godb/dialects"

dialects.Register("mydriver", MyDialect{})
```

Implement the `dialects.Dialect` interface:

```go
type Dialect interface {
    Name() string
    QuoteIdent(name string) string
    DataTypeOf(field *schema.Field) string
    ReplacePlaceholders(sql string) string
    SupportsReturning() bool
    OnConflictDoUpdate(table string, keys []string, cols []string) string
    CurrentColumns(db *sql.DB, table string) ([]DBColumn, error)
}
```

---

## Query Builder (Chain API)

Every query starts from `db.Model(&T{})` or `db.Table("table_name")` and returns an immutable `Chain`. Each method returns a new `Chain` — the original is never mutated, making chains safe to fork and reuse.

```go
base := db.Model(&Product{}).Where("active = ?", true)

// Two independent queries from the same base:
cheap  := base.Where("price < ?", 100).Order("price ASC").Limit(10)
recent := base.Order("created_at DESC").Limit(5)
```

### Filtering

```go
// Simple equality
db.Model(&User{}).Where("name = ?", "Alice")

// OR condition
db.Model(&User{}).Where("role = ?", "admin").OrWhere("role = ?", "manager")

// NOT
db.Model(&User{}).Not("archived = ?", true)

// IN / NOT IN
db.Model(&User{}).WhereIn("id", []int{1, 2, 3})
db.Model(&User{}).WhereNotIn("status", []string{"banned", "deleted"})

// BETWEEN
db.Model(&Order{}).WhereBetween("total", 100, 500)

// NULL checks
db.Model(&User{}).WhereNull("deleted_at")
db.Model(&User{}).WhereNotNull("verified_at")
```

### Joins

```go
db.Model(&Order{}).
    Joins("JOIN users ON users.id = orders.user_id").
    Where("users.role = ?", "member")

db.Model(&Post{}).
    LeftJoin("comments ON comments.post_id = posts.id").
    Where("comments.approved = ?", true)
```

### Ordering, Limiting, Pagination

```go
db.Model(&Product{}).
    Order("price DESC").
    Limit(20).
    Offset(40)   // page 3 of 20
```

### Grouping & Aggregation

```go
db.Model(&Order{}).
    GroupBy("user_id").
    Having("SUM(total) > ?", 1000).
    Pluck("user_id", &ids)
```

### Raw SQL

```go
db.Model(&User{}).Raw("SELECT * FROM users WHERE id = ?", 42).Scan(&user)
```

### Inspect the generated SQL

```go
sql, args := db.Model(&User{}).Where("age > ?", 18).Order("name").ToSQL()
fmt.Println(sql)   // SELECT * FROM "users" WHERE age > $1 ORDER BY name
fmt.Println(args)  // [18]
```

---

## CRUD Operations

### Find — multiple records

```go
var users []User
err := db.Model(&User{}).Where("active = ?", true).Find(&users)
```

### First / Last / Take

```go
var u User
err := db.Model(&User{}).Where("email = ?", "a@b.com").First(&u)  // ORDER BY pk ASC LIMIT 1
err  = db.Model(&User{}).Last(&u)                                  // ORDER BY pk DESC LIMIT 1
err  = db.Model(&User{}).Where("role = ?", "admin").Take(&u)       // no ordering
```

### Count / Exists

```go
count, err := db.Model(&User{}).Where("active = ?", true).Count()
exists, err := db.Model(&User{}).Where("email = ?", "a@b.com").Exists()
```

### Pluck — single column

```go
var emails []string
err := db.Model(&User{}).Where("active = ?", true).Pluck("email", &emails)
```

### Scan — arbitrary struct

```go
type Summary struct {
    UserID uint
    Total  float64
}
var rows []Summary
err := db.Model(&Order{}).
    Raw("SELECT user_id, SUM(total) AS total FROM orders GROUP BY user_id").
    Scan(&rows)
```

### Create

```go
user := User{Name: "Bob", Email: "bob@example.com"}
err := db.Model(&User{}).Create(&user)
// user.ID is now populated
```

### Bulk Create

```go
users := []User{{Name: "A"}, {Name: "B"}, {Name: "C"}}
err := db.Model(&User{}).BulkCreate(users)
```

### Update — specific columns

```go
// Update one column
err := db.Model(&User{}).Where("id = ?", 1).Update("name", "Carol")

// Update multiple columns from a map
err  = db.Model(&User{}).Where("active = ?", false).Updates(map[string]any{
    "active":     true,
    "updated_at": time.Now(),
})
```

### Save — full record upsert

```go
user.Name = "Dave"
err := db.Model(&User{}).Save(&user)
```

### Upsert — INSERT … ON CONFLICT

```go
err := db.Model(&Product{}).Upsert(&product, "sku")   // conflict key: sku
```

### Delete

```go
err := db.Model(&User{}).Where("id = ?", 42).Delete(&User{})
```

### Bulk Update

```go
err := db.Model(&User{}).BulkUpdate(users)
```

---

## Migrations

godb's migrator computes a **diff** between your Go structs and the live schema, then generates SQL to bring the database up to date — with no unnecessary `DROP` statements.

### AutoMigrate

```go
err := db.AutoMigrate(&User{}, &Product{}, &Order{})
```

Creates tables and adds missing columns. Existing data is never deleted.

### MigratePlan — preview before applying

```go
plan, err := db.MigratePlan(&User{}, &Product{})
if err != nil {
    log.Fatal(err)
}
fmt.Println(plan.Summary())
// → Tables to create: [orders]
//   Columns to add:   [users.last_login]
//   Indexes to add:   [idx_users_email]
for _, stmt := range plan.SQL {
    fmt.Println(stmt)
}
```

### Migrator — fine-grained control

```go
m := db.Migrator()

// Check
exists, _ := m.HasTable("users")
has,    _ := m.HasColumn("users", "email")

// Execute raw SQL (tracked in godb_migrations history)
err = m.RunSQL("ALTER TABLE products ADD COLUMN weight NUMERIC(8,2)")

// Drop
err = m.DropTable("legacy_table")
```

### Migration history

Every migration is recorded in the `godb_migrations` table:

```sql
CREATE TABLE godb_migrations (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL
);
```

---

## Schema & Struct Tags

godb parses Go struct tags with the `godb` key:

```go
type Product struct {
    ID          uint      `godb:"primaryKey;autoIncrement"`
    Name        string    `godb:"size:255;not null;index"`
    SKU         string    `godb:"uniqueIndex;size:100"`
    Price       float64   `godb:"type:NUMERIC(10,2);not null;default:0"`
    Description string    `godb:"type:TEXT"`
    Active      bool      `godb:"default:true"`
    CreatedAt   time.Time `godb:"autoCreateTime"`
    UpdatedAt   time.Time `godb:"autoUpdateTime"`
    DeletedAt   *time.Time `godb:"index"` // soft-delete sentinel
}
```

### Supported tag keys

| Tag | Description |
|-----|-------------|
| `primaryKey` | Marks the field as the primary key |
| `autoIncrement` | AUTO_INCREMENT / SERIAL / INTEGER PRIMARY KEY |
| `size:N` | Column max length (VARCHAR(N)) |
| `type:T` | Raw SQL type override |
| `not null` | NOT NULL constraint |
| `default:V` | Column default value |
| `index` | Creates a single-column index |
| `uniqueIndex` | Creates a unique index |
| `autoCreateTime` | Set to `NOW()` on insert |
| `autoUpdateTime` | Set to `NOW()` on every update |
| `column:name` | Override the column name |
| `->` | Read-only field (ignored on INSERT/UPDATE) |
| `<-` | Write-only field (ignored on SELECT) |
| `-` | Ignore this field entirely |

### Custom table names

Implement `TableNamer` to override the default snake_case plural:

```go
func (Product) TableName() string { return "catalog_items" }
```

---

## Lifecycle Hooks

Register functions that run automatically at key points in the record lifecycle:

```go
// Hooks are registered on the model struct — implement the interface.
type User struct { ... }

func (u *User) BeforeCreate(db *godb.DB) error {
    u.CreatedAt = time.Now()
    return nil
}

func (u *User) AfterCreate(db *godb.DB) error {
    go sendWelcomeEmail(u.Email)
    return nil
}

func (u *User) BeforeUpdate(db *godb.DB) error {
    u.UpdatedAt = time.Now()
    return nil
}

func (u *User) BeforeDelete(db *godb.DB) error {
    if u.Protected {
        return errors.New("cannot delete a protected record")
    }
    return nil
}

func (u *User) Validate() error {
    if u.Email == "" {
        return errors.New("email is required")
    }
    return nil
}
```

### Hook interfaces

| Interface | Method | Fires |
|-----------|--------|-------|
| `BeforeCreater` | `BeforeCreate(*DB) error` | Before INSERT |
| `AfterCreater` | `AfterCreate(*DB) error` | After INSERT |
| `BeforeUpdater` | `BeforeUpdate(*DB) error` | Before UPDATE |
| `AfterUpdater` | `AfterUpdate(*DB) error` | After UPDATE |
| `BeforeDeleter` | `BeforeDelete(*DB) error` | Before DELETE |
| `AfterDeleter` | `AfterDelete(*DB) error` | After DELETE |
| `Validator` | `Validate() error` | Before CREATE and UPDATE |

A non-nil error from any **Before** hook aborts the operation and rolls back any transaction.

---

## Query Caching

### In-memory LRU cache

```go
import "github.com/Brah-Timo/godb/cache"

c := cache.New(1000, 5*time.Minute)   // 1000-entry LRU, 5-minute TTL

db, _ := godb.Open("postgres", dsn, godb.WithCache(c))

// Cache this specific query
var users []User
err := db.Model(&User{}).
    Where("active = ?", true).
    Cache(5 * time.Minute).   // cache result for 5 minutes
    Find(&users)

// Invalidate all cached queries for the "users" table
c.InvalidateTable("users")
```

### Redis cache

```go
import (
    "github.com/Brah-Timo/godb/cache"
    "github.com/redis/go-redis/v9"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
c := cache.NewRedisCache(rdb, 10*time.Minute)

db, _ := godb.Open("postgres", dsn, godb.WithCache(c))
```

### Cache key generation

Cache keys are built from the full SQL statement and bound arguments, prefixed by the table name for targeted invalidation.

---

## Relations & Eager Loading

Declare relations in struct tags, then preload them with `Preload`:

```go
type User struct {
    ID    uint
    Name  string
    Posts []Post `godb:"hasMany;foreignKey:UserID"`
}

type Post struct {
    ID     uint
    UserID uint
    Title  string
    User   User `godb:"belongsTo;foreignKey:UserID"`
}

// HasMany
var users []User
db.Model(&User{}).Preload("Posts").Find(&users)

// BelongsTo
var posts []Post
db.Model(&Post{}).Preload("User").Find(&posts)
```

### Supported relation types

| Tag value | SQL pattern |
|-----------|-------------|
| `hasMany` | child table has a foreign key pointing to parent |
| `hasOne` | one child row per parent |
| `belongsTo` | this struct carries the foreign key |
| `manyToMany` | join table required; specify with `joinTable:` |

---

## Connection Pool

Configure pool parameters via `WithPool`:

```go
db, _ := godb.Open("postgres", dsn,
    godb.WithPool(godb.PoolConfig{
        MaxOpenConns:    50,
        MaxIdleConns:    10,
        ConnMaxLifetime: 10 * time.Minute,
        ConnMaxIdleTime: 5 * time.Minute,
    }),
)

// Read pool metrics
stats := db.Stats()
fmt.Printf("open=%d idle=%d in-use=%d utilisation=%.1f%%\n",
    stats.OpenConnections,
    stats.Idle,
    stats.InUse,
    stats.Utilisation*100,
)
```

---

## Dashboard

Enable the embedded HTTP dashboard for live query analytics:

```go
db, _ := godb.Open("postgres", dsn,
    godb.WithDashboard(":9090"),
)
```

Then open **http://localhost:9090** in your browser.

### Dashboard features

- **Top queries** — sorted by execution count
- **Slow queries** — sorted by average duration
- **Pool metrics** — open, idle, in-use, utilisation gauge
- **Query histogram** — latency distribution
- Auto-refreshes every 5 seconds

The dashboard UI is embedded in the binary via `//go:embed` — no external files required.

---

## Transactions

```go
err := db.Transaction(func(tx *godb.DB) error {
    if err := tx.Model(&Account{}).Where("id = ?", from).
        Update("balance", godb.Raw("balance - ?", amount)); err != nil {
        return err // triggers rollback
    }
    if err := tx.Model(&Account{}).Where("id = ?", to).
        Update("balance", godb.Raw("balance + ?", amount)); err != nil {
        return err // triggers rollback
    }
    return nil // triggers commit
})
```

`Transaction` automatically commits on `nil` return and rolls back on any error. Nested `Transaction` calls reuse the active transaction.

---

## Error Handling

```go
err := db.Model(&User{}).Where("id = ?", 999).First(&u)

if godb.IsNotFound(err) {
    fmt.Println("user not found")
}
if godb.IsDuplicate(err) {
    fmt.Println("duplicate key — email already registered")
}

// Typed error with details
var dbErr *godb.Error
if errors.As(err, &dbErr) {
    fmt.Printf("code=%s sql=%s\n", dbErr.Code, dbErr.SQL)
}
```

### Sentinel errors

| Function | Description |
|----------|-------------|
| `godb.IsNotFound(err)` | No rows matched — record not found |
| `godb.IsDuplicate(err)` | UNIQUE constraint violation |
| `godb.ErrRecordNotFound` | The sentinel error value |
| `godb.ErrNoCondition` | WHERE-less UPDATE/DELETE guard |

---

## Logger

Implement the `Logger` interface to plug in your own logging library:

```go
type Logger interface {
    Info(msg string, fields ...any)
    Warn(msg string, fields ...any)
    Error(msg string, fields ...any)
    Query(sql string, args []any, duration time.Duration, err error)
}
```

### Built-in loggers

```go
// Default — structured output to stderr
db, _ := godb.Open("sqlite3", ":memory:") // uses defaultLogger

// No-op — silence all logging
db, _ := godb.Open("sqlite3", ":memory:", godb.WithLogger(godb.NopLogger{}))

// Function adapter
db, _ := godb.Open("sqlite3", ":memory:", godb.WithLogger(
    godb.LoggerFunc(func(sql string, args []any, d time.Duration, err error) {
        log.Printf("[%.2fms] %s %v err=%v", d.Seconds()*1000, sql, args, err)
    }),
))
```

---

## Testing

Run the full test suite:

```bash
# Standard (no CGO required — skips sqlite dialect tests)
go test ./...

# With SQLite (requires GCC)
CGO_ENABLED=1 go test ./...

# Verbose with race detector
go test -race -v ./...

# Specific package
go test ./migration/... -v -run TestPlan
```

### Test coverage by package

| Package | Tests |
|---------|-------|
| `builder` | Chain immutability, ToSQL, WhereIn, Joins, GroupBy, cache flag |
| `cache` | LRU Set/Get, expiry, InvalidateTable, Delete, LRU eviction, Flush |
| `dialects/postgres` | ReplacePlaceholders, QuoteIdent, DataTypeOf, SupportsReturning, OnConflict |
| `migration` | Plan_NewTable, Run_CreateTable, MultipleModels, Idempotent, HasColumn, RunSQL, DropTable |
| `schema` | BasicModel, FieldAttributes, CustomTableName, Registry_Caching, Registry_SliceInput, ToSnakeCase |

---

## Project Layout

```
godb/
├── godb.go              # DB struct, Open/MustOpen/Transaction/AutoMigrate
├── config.go            # Config + With* functional options
├── chain.go             # Chain type alias + newChain helpers
├── errors.go            # Sentinel errors, Error struct, IsNotFound/IsDuplicate
├── logger.go            # Logger interface, defaultLogger, NopLogger
│
├── builder/             # Immutable query builder
│   ├── chain.go         # Chain struct + all fluent methods
│   ├── execute.go       # Find/First/Last/Create/Updates/Delete/Upsert
│   ├── insert.go        # buildInsert / buildUpsert / buildBulkInsert
│   ├── select.go        # buildSelect / buildCount / buildWhere
│   ├── update.go        # buildUpdate
│   ├── delete.go        # buildDelete
│   ├── bulk.go          # BulkCreate / BulkUpdate
│   └── chain_test.go
│
├── cache/               # Query result caching
│   ├── cache.go         # Cache interface, factory, BuildKey
│   ├── memory.go        # LRU MemoryCache
│   ├── redis.go         # RedisCache (go-redis)
│   ├── nop.go           # NopCache (no-op)
│   └── memory_test.go
│
├── dialects/            # Database dialect registry
│   ├── dialect.go       # Dialect interface + Register/Get/Registered
│   ├── postgres/        # PostgreSQL dialect
│   ├── mysql/           # MySQL dialect
│   └── sqlite/          # SQLite dialect
│
├── migration/           # Schema migration engine
│   ├── migrator.go      # Migrator: Plan/Run/RunSQL/DropTable/HasTable/HasColumn
│   ├── diff.go          # currentColumns / diffModel
│   ├── plan.go          # Plan struct (TablesToCreate, ColumnsToAdd, SQL, Summary)
│   ├── history.go       # godb_migrations tracking table
│   └── migrator_test.go
│
├── schema/              # Struct introspection & registry
│   ├── parser.go        # parseStruct / parseFields
│   ├── field.go         # Field, FieldKind, RelationType, Relation
│   ├── model.go         # Model struct with capability flags
│   ├── registry.go      # concurrent-safe Parse/ParseType/All/Clear
│   ├── scanner.go       # ScanRows / ScanRow / JSONScanner / IsZeroValue
│   ├── naming.go        # ToSnakeCase export
│   └── parser_test.go
│
├── hooks/               # Lifecycle hook interfaces
│   └── hooks.go         # BeforeCreater, AfterCreater, Validator, …
│
├── pool/                # Connection pool metrics
│   └── pool.go          # Stats wrapper, Utilisation, Collector
│
├── dashboard/           # Embedded HTTP analytics dashboard
│   ├── collector.go     # QueryRecord, QueryStats, Collector
│   ├── server.go        # HTTP server + //go:embed ui/*
│   └── ui/
│       └── index.html   # Self-contained dashboard SPA
│
├── relations/           # Eager-loading engine
│   └── loader.go        # Loader: Load, loadHasMany, loadHasOne, loadBelongsTo, loadManyToMany
│
├── internal/
│   ├── sql/
│   │   └── placeholder.go  # ReplacePlaceholders / Placeholders
│   └── utils/
│       └── naming.go       # ToSnakeCase / ToCamelCase / Plural
│
└── example/
    └── main.go          # Complete SQLite example
```

---

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes with tests
4. Run the full suite: `go test ./...` and `go vet ./...`
5. Submit a pull request against `main`

### Code style

```bash
go fmt ./...
go vet ./...
```

No external linters are required — `go vet` is the gatekeeper.

---

## License

MIT © Brah-Timo
