package migration

import (
	"context"
	"database/sql"
	"strings"

	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  Schema diff
// ─────────────────────────────────────────────────────────────

// currentColumns reads the actual columns present in the database for table.
// Returns a map of column_name → DBColumn, or nil if the table does not exist.
//
// It handles two query formats:
//  1. Standard (Postgres/MySQL): SELECT column_name, data_type, is_nullable, column_default
//  2. SQLite PRAGMA table_info: (cid INTEGER, name TEXT, type TEXT, notnull INTEGER, dflt_value TEXT, pk INTEGER)
func currentColumns(ctx context.Context, db *sql.DB, d dialects.Dialect, table string) (map[string]dialects.DBColumn, error) {
	// Check table existence
	existsSQL := d.TableExistsSQL(table)
	rows, err := db.QueryContext(ctx, existsSQL)
	if err != nil {
		return nil, err
	}
	exists := rows.Next()
	rows.Close()
	if !exists {
		return nil, nil // table does not exist
	}

	// Read columns
	colsSQL := d.ColumnsSQL(table)
	crows, err := db.QueryContext(ctx, colsSQL)
	if err != nil {
		return nil, err
	}
	defer crows.Close()

	// Detect query format from column names
	colNames, err := crows.Columns()
	if err != nil {
		return nil, err
	}

	cols := make(map[string]dialects.DBColumn)

	// Check if this looks like SQLite PRAGMA table_info (has "cid" as first column)
	isSQLitePragma := len(colNames) >= 6 && strings.EqualFold(colNames[0], "cid")

	if isSQLitePragma {
		// SQLite PRAGMA table_info: cid, name, type, notnull, dflt_value, pk
		for crows.Next() {
			var cid int
			var name, typ string
			var notNull int
			var dfltValue sql.NullString
			var pk int
			if err := crows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
				return nil, err
			}
			c := dialects.DBColumn{
				Name:       name,
				DataType:   typ,
				IsNullable: notNull == 0,
				IsPrimary:  pk > 0,
			}
			if dfltValue.Valid {
				s := dfltValue.String
				c.Default = &s
			}
			cols[strings.ToLower(name)] = c
		}
	} else {
		// Standard format: column_name, data_type, is_nullable, column_default
		for crows.Next() {
			var c dialects.DBColumn
			var isNull interface{}
			var dflt sql.NullString
			if err := crows.Scan(&c.Name, &c.DataType, &isNull, &dflt); err != nil {
				continue // gracefully skip unparseable rows
			}
			switch v := isNull.(type) {
			case bool:
				c.IsNullable = v
			case int64:
				c.IsNullable = v == 1
			case []byte:
				c.IsNullable = strings.ToUpper(string(v)) == "YES"
			case string:
				c.IsNullable = strings.ToUpper(v) == "YES"
			}
			if dflt.Valid {
				s := dflt.String
				c.Default = &s
			}
			cols[strings.ToLower(c.Name)] = c
		}
	}

	return cols, crows.Err()
}

// diffModel compares the desired model against current DB state and
// returns the DDL statements needed to reconcile them.
func diffModel(
	ctx context.Context,
	db *sql.DB,
	d dialects.Dialect,
	m *schema.Model,
) (plan *Plan, err error) {
	plan = &Plan{}

	existing, err := currentColumns(ctx, db, d, m.Table)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		// Table does not exist → CREATE TABLE
		plan.TablesToCreate = append(plan.TablesToCreate, m.Table)
		plan.SQL = append(plan.SQL, d.CreateTableSQL(m))

		// Indexes
		for _, f := range m.ColumnFields() {
			if f.HasIndex || f.Unique {
				idxSQL := d.CreateIndexSQL(m.Table, f)
				plan.IndexesToCreate = append(plan.IndexesToCreate, IndexDiff{
					Table:  m.Table,
					Column: f.Column,
					Unique: f.Unique,
					SQL:    idxSQL,
				})
				plan.SQL = append(plan.SQL, idxSQL)
			}
		}
		return plan, nil
	}

	// Table exists → look for missing columns only (never drop)
	for _, f := range m.ColumnFields() {
		if _, ok := existing[strings.ToLower(f.Column)]; !ok {
			addSQL := d.AddColumnSQL(m.Table, f)
			plan.ColumnsToAdd = append(plan.ColumnsToAdd, ColumnDiff{
				Table:  m.Table,
				Column: f.Column,
				Type:   d.DataTypeOf(f),
				SQL:    addSQL,
			})
			plan.SQL = append(plan.SQL, addSQL)

			// Add index if needed
			if f.HasIndex || f.Unique {
				idxSQL := d.CreateIndexSQL(m.Table, f)
				plan.IndexesToCreate = append(plan.IndexesToCreate, IndexDiff{
					Table:  m.Table,
					Column: f.Column,
					Unique: f.Unique,
					SQL:    idxSQL,
				})
				plan.SQL = append(plan.SQL, idxSQL)
			}
		}
	}

	return plan, nil
}
