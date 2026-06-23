// Package sqlite registers the SQLite3 dialect with godb.
// Import it for its side-effect:
//
//	import _ "github.com/Brah-Timo/godb/dialects/sqlite"
//
// Note: SQLite requires CGO (github.com/mattn/go-sqlite3).
package sqlite

import (
	"fmt"
	"strings"

	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

func init() {
	dialects.Register(&SQLiteDialect{})
}

// SQLiteDialect implements dialects.Dialect for SQLite 3.
type SQLiteDialect struct{}

func (d *SQLiteDialect) Name() string { return "sqlite3" }

// ─────────────────────────────────────────────────────────────
//  Placeholders
// ─────────────────────────────────────────────────────────────

// SQLite uses ? natively; ReplacePlaceholders is a no-op.
func (d *SQLiteDialect) ReplacePlaceholders(sql string, _ int) string { return sql }

// ─────────────────────────────────────────────────────────────
//  Quoting
// ─────────────────────────────────────────────────────────────

func (d *SQLiteDialect) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ─────────────────────────────────────────────────────────────
//  DDL — types
// ─────────────────────────────────────────────────────────────

// SQLite uses dynamic typing; we still emit column affinities so that
// CHECK constraints and future tooling work correctly.
func (d *SQLiteDialect) DataTypeOf(f *schema.Field) string {
	if f.DBType != "" {
		return f.DBType
	}
	switch f.Kind {
	case schema.KindInt:
		return "INTEGER"
	case schema.KindFloat:
		return "REAL"
	case schema.KindString:
		return "TEXT"
	case schema.KindBool:
		return "INTEGER" // 0 or 1
	case schema.KindTime:
		return "DATETIME"
	case schema.KindBytes:
		return "BLOB"
	case schema.KindJSON:
		return "TEXT" // stored as JSON text
	default:
		return "TEXT"
	}
}

// ─────────────────────────────────────────────────────────────
//  DDL — statements
// ─────────────────────────────────────────────────────────────

func (d *SQLiteDialect) CreateTableSQL(m *schema.Model) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE IF NOT EXISTS %s (\n", d.QuoteIdent(m.Table))

	cols := m.ColumnFields()
	for i, f := range cols {
		sb.WriteString("  ")
		sb.WriteString(d.QuoteIdent(f.Column))
		sb.WriteByte(' ')
		sb.WriteString(d.DataTypeOf(f))

		if f.PrimaryKey {
			sb.WriteString(" PRIMARY KEY")
			if f.AutoIncrement {
				sb.WriteString(" AUTOINCREMENT")
			}
		}
		if f.NotNull && !f.PrimaryKey {
			sb.WriteString(" NOT NULL")
		}
		if f.Unique && !f.PrimaryKey {
			sb.WriteString(" UNIQUE")
		}
		if f.Default != "" {
			fmt.Fprintf(&sb, " DEFAULT %s", f.Default)
		}

		if i < len(cols)-1 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(")")
	return sb.String()
}

// SQLite does not support ADD COLUMN IF NOT EXISTS; we always ADD COLUMN.
func (d *SQLiteDialect) AddColumnSQL(table string, f *schema.Field) string {
	constraint := ""
	if f.NotNull && f.Default != "" {
		// SQLite requires a default when adding NOT NULL column to existing table
		constraint = fmt.Sprintf(" NOT NULL DEFAULT %s", f.Default)
	} else if f.Default != "" {
		constraint = " DEFAULT " + f.Default
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s",
		d.QuoteIdent(table), d.QuoteIdent(f.Column), d.DataTypeOf(f), constraint)
}

// SQLite does not support ALTER COLUMN; we return a comment to inform.
func (d *SQLiteDialect) ModifyColumnSQL(table string, f *schema.Field) string {
	return fmt.Sprintf("-- SQLite does not support MODIFY COLUMN for %s.%s; recreate the table manually", table, f.Column)
}

func (d *SQLiteDialect) DropColumnSQL(table, column string) string {
	// Supported since SQLite 3.35.0 (2021-03-12)
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
		d.QuoteIdent(table), d.QuoteIdent(column))
}

func (d *SQLiteDialect) CreateIndexSQL(table string, f *schema.Field) string {
	unique := ""
	if f.Unique {
		unique = "UNIQUE "
	}
	idxName := fmt.Sprintf("idx_%s_%s", table, f.Column)
	return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		unique, d.QuoteIdent(idxName), d.QuoteIdent(table), d.QuoteIdent(f.Column))
}

// ─────────────────────────────────────────────────────────────
//  Schema introspection
// ─────────────────────────────────────────────────────────────

func (d *SQLiteDialect) TableExistsSQL(table string) string {
	return fmt.Sprintf(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='%s'", table)
}

func (d *SQLiteDialect) ColumnsSQL(table string) string {
	return fmt.Sprintf("PRAGMA table_info(%s)", d.QuoteIdent(table))
}

func (d *SQLiteDialect) IndexesSQL(table string) string {
	return fmt.Sprintf("PRAGMA index_list(%s)", d.QuoteIdent(table))
}

// ─────────────────────────────────────────────────────────────
//  Advanced
// ─────────────────────────────────────────────────────────────

// SQLite supports RETURNING since version 3.35.0.
func (d *SQLiteDialect) SupportsReturning() bool { return true }

func (d *SQLiteDialect) OnConflictDoUpdate(conflictCols, updateCols []string) string {
	conflict := make([]string, len(conflictCols))
	for i, c := range conflictCols {
		conflict[i] = d.QuoteIdent(c)
	}
	updates := make([]string, len(updateCols))
	for i, c := range updateCols {
		q := d.QuoteIdent(c)
		updates[i] = fmt.Sprintf("%s = excluded.%s", q, q)
	}
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s",
		strings.Join(conflict, ", "), strings.Join(updates, ", "))
}

func (d *SQLiteDialect) LimitOffset(limit, offset int) string {
	if limit > 0 && offset > 0 {
		return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
	}
	if limit > 0 {
		return fmt.Sprintf("LIMIT %d", limit)
	}
	if offset > 0 {
		return fmt.Sprintf("OFFSET %d", offset)
	}
	return ""
}
