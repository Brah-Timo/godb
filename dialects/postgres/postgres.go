// Package postgres registers the PostgreSQL dialect with godb.
// Import it for its side-effect:
//
//	import _ "github.com/Brah-Timo/godb/dialects/postgres"
package postgres

import (
	"fmt"
	"strings"

	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

func init() {
	dialects.Register(&PostgresDialect{})
}

// PostgresDialect implements dialects.Dialect for PostgreSQL.
type PostgresDialect struct{}

func (d *PostgresDialect) Name() string { return "postgres" }

// ─────────────────────────────────────────────────────────────
//  Placeholders
// ─────────────────────────────────────────────────────────────

// ReplacePlaceholders converts ? → $1, $2, … as required by lib/pq.
func (d *PostgresDialect) ReplacePlaceholders(sql string, startAt int) string {
	var b strings.Builder
	b.Grow(len(sql) + 10)
	idx := startAt
	for _, ch := range sql {
		if ch == '?' {
			fmt.Fprintf(&b, "$%d", idx)
			idx++
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────
//  Quoting
// ─────────────────────────────────────────────────────────────

func (d *PostgresDialect) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ─────────────────────────────────────────────────────────────
//  DDL — types
// ─────────────────────────────────────────────────────────────

func (d *PostgresDialect) DataTypeOf(f *schema.Field) string {
	if f.DBType != "" {
		return f.DBType
	}
	switch f.Kind {
	case schema.KindInt:
		if f.AutoIncrement {
			return "BIGSERIAL"
		}
		return "BIGINT"
	case schema.KindFloat:
		if f.Precision > 0 {
			return fmt.Sprintf("NUMERIC(%d,%d)", f.Precision, f.Scale)
		}
		return "DOUBLE PRECISION"
	case schema.KindString:
		if f.Size > 0 {
			return fmt.Sprintf("VARCHAR(%d)", f.Size)
		}
		return "TEXT"
	case schema.KindBool:
		return "BOOLEAN"
	case schema.KindTime:
		return "TIMESTAMPTZ"
	case schema.KindBytes:
		return "BYTEA"
	case schema.KindJSON:
		return "JSONB"
	default:
		return "TEXT"
	}
}

// ─────────────────────────────────────────────────────────────
//  DDL — statements
// ─────────────────────────────────────────────────────────────

func (d *PostgresDialect) CreateTableSQL(m *schema.Model) string {
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

func (d *PostgresDialect) AddColumnSQL(table string, f *schema.Field) string {
	constraint := ""
	if f.NotNull {
		constraint += " NOT NULL"
	}
	if f.Unique {
		constraint += " UNIQUE"
	}
	if f.Default != "" {
		constraint += " DEFAULT " + f.Default
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s%s",
		d.QuoteIdent(table), d.QuoteIdent(f.Column), d.DataTypeOf(f), constraint)
}

func (d *PostgresDialect) ModifyColumnSQL(table string, f *schema.Field) string {
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s",
		d.QuoteIdent(table), d.QuoteIdent(f.Column), d.DataTypeOf(f))
}

func (d *PostgresDialect) DropColumnSQL(table, column string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s",
		d.QuoteIdent(table), d.QuoteIdent(column))
}

func (d *PostgresDialect) CreateIndexSQL(table string, f *schema.Field) string {
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

func (d *PostgresDialect) TableExistsSQL(table string) string {
	return fmt.Sprintf(
		"SELECT 1 FROM information_schema.tables WHERE table_name = '%s' AND table_schema = current_schema() LIMIT 1",
		table)
}

func (d *PostgresDialect) ColumnsSQL(table string) string {
	return fmt.Sprintf(`
SELECT
    column_name,
    data_type,
    is_nullable = 'YES' AS is_nullable,
    column_default
FROM information_schema.columns
WHERE table_name = '%s'
  AND table_schema = current_schema()
ORDER BY ordinal_position`, table)
}

func (d *PostgresDialect) IndexesSQL(table string) string {
	return fmt.Sprintf(`
SELECT
    indexname AS index_name,
    indexdef  AS index_def
FROM pg_indexes
WHERE tablename = '%s'
  AND schemaname = current_schema()`, table)
}

// ─────────────────────────────────────────────────────────────
//  Advanced
// ─────────────────────────────────────────────────────────────

func (d *PostgresDialect) SupportsReturning() bool { return true }

func (d *PostgresDialect) OnConflictDoUpdate(conflictCols, updateCols []string) string {
	conflict := make([]string, len(conflictCols))
	for i, c := range conflictCols {
		conflict[i] = d.QuoteIdent(c)
	}
	updates := make([]string, len(updateCols))
	for i, c := range updateCols {
		q := d.QuoteIdent(c)
		updates[i] = fmt.Sprintf("%s = EXCLUDED.%s", q, q)
	}
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s",
		strings.Join(conflict, ", "), strings.Join(updates, ", "))
}

func (d *PostgresDialect) LimitOffset(limit, offset int) string {
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
