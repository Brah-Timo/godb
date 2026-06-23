// Package mysql registers the MySQL / MariaDB dialect with godb.
// Import it for its side-effect:
//
//	import _ "github.com/Brah-Timo/godb/dialects/mysql"
package mysql

import (
	"fmt"
	"strings"

	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

func init() {
	dialects.Register(&MySQLDialect{})
}

// MySQLDialect implements dialects.Dialect for MySQL / MariaDB.
type MySQLDialect struct{}

func (d *MySQLDialect) Name() string { return "mysql" }

// ─────────────────────────────────────────────────────────────
//  Placeholders
// ─────────────────────────────────────────────────────────────

// MySQL uses ? natively; ReplacePlaceholders is a no-op.
func (d *MySQLDialect) ReplacePlaceholders(sql string, _ int) string { return sql }

// ─────────────────────────────────────────────────────────────
//  Quoting
// ─────────────────────────────────────────────────────────────

func (d *MySQLDialect) QuoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// ─────────────────────────────────────────────────────────────
//  DDL — types
// ─────────────────────────────────────────────────────────────

func (d *MySQLDialect) DataTypeOf(f *schema.Field) string {
	if f.DBType != "" {
		return f.DBType
	}
	switch f.Kind {
	case schema.KindInt:
		if f.AutoIncrement {
			return "BIGINT UNSIGNED AUTO_INCREMENT"
		}
		return "BIGINT"
	case schema.KindFloat:
		if f.Precision > 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", f.Precision, f.Scale)
		}
		return "DOUBLE"
	case schema.KindString:
		if f.Size > 0 {
			return fmt.Sprintf("VARCHAR(%d)", f.Size)
		}
		return "LONGTEXT"
	case schema.KindBool:
		return "TINYINT(1)"
	case schema.KindTime:
		return "DATETIME(6)"
	case schema.KindBytes:
		return "LONGBLOB"
	case schema.KindJSON:
		return "JSON"
	default:
		return "TEXT"
	}
}

// ─────────────────────────────────────────────────────────────
//  DDL — statements
// ─────────────────────────────────────────────────────────────

func (d *MySQLDialect) CreateTableSQL(m *schema.Model) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE IF NOT EXISTS %s (\n", d.QuoteIdent(m.Table))

	cols := m.ColumnFields()
	var pks []string

	for i, f := range cols {
		sb.WriteString("  ")
		sb.WriteString(d.QuoteIdent(f.Column))
		sb.WriteByte(' ')
		sb.WriteString(d.DataTypeOf(f))

		if f.NotNull {
			sb.WriteString(" NOT NULL")
		}
		if f.Unique && !f.PrimaryKey {
			sb.WriteString(" UNIQUE")
		}
		if f.Default != "" {
			fmt.Fprintf(&sb, " DEFAULT %s", f.Default)
		}
		if f.PrimaryKey {
			pks = append(pks, d.QuoteIdent(f.Column))
		}

		if i < len(cols)-1 || len(pks) > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}

	if len(pks) > 0 {
		fmt.Fprintf(&sb, "  PRIMARY KEY (%s)\n", strings.Join(pks, ", "))
	}
	sb.WriteString(") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci")
	return sb.String()
}

func (d *MySQLDialect) AddColumnSQL(table string, f *schema.Field) string {
	constraint := ""
	if f.NotNull {
		constraint += " NOT NULL"
	}
	if f.Default != "" {
		constraint += " DEFAULT " + f.Default
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s",
		d.QuoteIdent(table), d.QuoteIdent(f.Column), d.DataTypeOf(f), constraint)
}

func (d *MySQLDialect) ModifyColumnSQL(table string, f *schema.Field) string {
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s",
		d.QuoteIdent(table), d.QuoteIdent(f.Column), d.DataTypeOf(f))
}

func (d *MySQLDialect) DropColumnSQL(table, column string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
		d.QuoteIdent(table), d.QuoteIdent(column))
}

func (d *MySQLDialect) CreateIndexSQL(table string, f *schema.Field) string {
	unique := ""
	if f.Unique {
		unique = "UNIQUE "
	}
	idxName := fmt.Sprintf("idx_%s_%s", table, f.Column)
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
		unique, d.QuoteIdent(idxName), d.QuoteIdent(table), d.QuoteIdent(f.Column))
}

// ─────────────────────────────────────────────────────────────
//  Schema introspection
// ─────────────────────────────────────────────────────────────

func (d *MySQLDialect) TableExistsSQL(table string) string {
	return fmt.Sprintf("SHOW TABLES LIKE '%s'", table)
}

func (d *MySQLDialect) ColumnsSQL(table string) string {
	return fmt.Sprintf(`
SELECT
    COLUMN_NAME        AS column_name,
    DATA_TYPE          AS data_type,
    IS_NULLABLE = 'YES' AS is_nullable,
    COLUMN_DEFAULT     AS column_default
FROM information_schema.COLUMNS
WHERE TABLE_NAME   = '%s'
  AND TABLE_SCHEMA = DATABASE()
ORDER BY ORDINAL_POSITION`, table)
}

func (d *MySQLDialect) IndexesSQL(table string) string {
	return fmt.Sprintf(`
SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE
FROM information_schema.STATISTICS
WHERE TABLE_NAME   = '%s'
  AND TABLE_SCHEMA = DATABASE()`, table)
}

// ─────────────────────────────────────────────────────────────
//  Advanced
// ─────────────────────────────────────────────────────────────

func (d *MySQLDialect) SupportsReturning() bool { return false }

func (d *MySQLDialect) OnConflictDoUpdate(_, updateCols []string) string {
	parts := make([]string, len(updateCols))
	for i, c := range updateCols {
		q := d.QuoteIdent(c)
		parts[i] = fmt.Sprintf("%s = VALUES(%s)", q, q)
	}
	return "ON DUPLICATE KEY UPDATE " + strings.Join(parts, ", ")
}

func (d *MySQLDialect) LimitOffset(limit, offset int) string {
	if limit > 0 && offset > 0 {
		return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
	}
	if limit > 0 {
		return fmt.Sprintf("LIMIT %d", limit)
	}
	// MySQL requires LIMIT with OFFSET; use a very large number
	if offset > 0 {
		return fmt.Sprintf("LIMIT 18446744073709551615 OFFSET %d", offset)
	}
	return ""
}
