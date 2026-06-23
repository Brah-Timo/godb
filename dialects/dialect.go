// Package dialects defines the Dialect interface and the driver registry.
//
// Each supported database implements the Dialect interface in its own sub-package.
// The dialects are registered via init() functions in those sub-packages,
// so importing the sub-package (or the side-effect import in godb.go) is all
// that is needed to make them available to dialects.Get().
package dialects

import (
	"fmt"
	"sync"

	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  Dialect interface
// ─────────────────────────────────────────────────────────────

// Dialect abstracts the SQL differences between database engines.
// Every method must be goroutine-safe and stateless.
type Dialect interface {
	// Name returns the driver string ("postgres", "mysql", "sqlite3").
	Name() string

	// ── Placeholder style ────────────────────────────────────
	// ReplacePlaceholders rewrites the ? placeholders in sql to the
	// dialect's native format, starting the counter at startAt.
	//   PostgreSQL: ? → $1, $2, …
	//   MySQL:      ? → ? (no-op)
	//   SQLite:     ? → ? (no-op)
	ReplacePlaceholders(sql string, startAt int) string

	// ── Quoting ──────────────────────────────────────────────
	// QuoteIdent wraps an identifier (table or column name) in dialect-
	// appropriate quotes.
	//   PostgreSQL / SQLite: "identifier"
	//   MySQL:               `identifier`
	QuoteIdent(name string) string

	// ── DDL generation ───────────────────────────────────────
	// DataTypeOf returns the SQL column type for the given schema.Field.
	DataTypeOf(f *schema.Field) string

	// CreateTableSQL returns the full CREATE TABLE IF NOT EXISTS statement.
	CreateTableSQL(m *schema.Model) string

	// AddColumnSQL returns ALTER TABLE … ADD COLUMN …
	AddColumnSQL(table string, f *schema.Field) string

	// ModifyColumnSQL returns ALTER TABLE … MODIFY/ALTER COLUMN …
	ModifyColumnSQL(table string, f *schema.Field) string

	// DropColumnSQL returns ALTER TABLE … DROP COLUMN …
	DropColumnSQL(table, column string) string

	// CreateIndexSQL returns CREATE [UNIQUE] INDEX IF NOT EXISTS …
	CreateIndexSQL(table string, f *schema.Field) string

	// ── Schema introspection ─────────────────────────────────
	// TableExistsSQL returns a query that yields at least one row if the
	// table exists, zero rows otherwise.
	TableExistsSQL(table string) string

	// ColumnsSQL returns a query whose rows can be scanned into DBColumn.
	ColumnsSQL(table string) string

	// IndexesSQL returns a query whose rows describe existing indexes.
	IndexesSQL(table string) string

	// ── Advanced features ────────────────────────────────────
	// SupportsReturning reports whether the database supports the RETURNING
	// clause (PostgreSQL, SQLite ≥ 3.35).
	SupportsReturning() bool

	// OnConflictDoUpdate returns the dialect-specific UPSERT suffix.
	//   PostgreSQL: ON CONFLICT (cols…) DO UPDATE SET …
	//   MySQL:      ON DUPLICATE KEY UPDATE …
	//   SQLite:     ON CONFLICT (cols…) DO UPDATE SET …
	OnConflictDoUpdate(conflictCols []string, updateCols []string) string

	// LimitOffset returns the SQL fragment for LIMIT n OFFSET m.
	LimitOffset(limit, offset int) string
}

// ─────────────────────────────────────────────────────────────
//  DBColumn — result of ColumnsSQL scan
// ─────────────────────────────────────────────────────────────

// DBColumn holds introspected column metadata from the running database.
type DBColumn struct {
	Name       string
	DataType   string
	IsNullable bool
	Default    *string
	IsPrimary  bool
}

// ─────────────────────────────────────────────────────────────
//  Registry
// ─────────────────────────────────────────────────────────────

var (
	mu       sync.RWMutex
	registry = make(map[string]Dialect)
)

// Register adds a Dialect implementation to the global registry.
// Typically called from an init() in a dialect sub-package.
func Register(d Dialect) {
	mu.Lock()
	registry[d.Name()] = d
	mu.Unlock()
}

// Get retrieves the Dialect for driver, or returns ErrUnsupportedDialect.
func Get(driver string) (Dialect, error) {
	mu.RLock()
	d, ok := registry[driver]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("godb: unsupported dialect %q — import the dialect package or use postgres/mysql/sqlite3", driver)
	}
	return d, nil
}

// Registered returns all registered dialect names.
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}
