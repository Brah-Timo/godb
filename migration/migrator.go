package migration

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  Migrator
// ─────────────────────────────────────────────────────────────

// Migrator orchestrates schema migrations for a godb.DB.
type Migrator struct {
	db       *sql.DB
	dialect  dialects.Dialect
	registry *schema.Registry
}

// NewMigrator creates a new Migrator. Called internally by DB.AutoMigrate.
func NewMigrator(db *sql.DB, d dialects.Dialect, r *schema.Registry) *Migrator {
	return &Migrator{db: db, dialect: d, registry: r}
}

// Plan analyses the given model structs and returns the migration plan
// without applying any changes to the database.
func (m *Migrator) Plan(ctx context.Context, models ...interface{}) (*Plan, error) {
	combined := &Plan{}
	for _, model := range models {
		s, err := m.registry.Parse(model)
		if err != nil {
			return nil, err
		}
		p, err := diffModel(ctx, m.db, m.dialect, s)
		if err != nil {
			return nil, fmt.Errorf("migration.Plan(%s): %w", s.Name, err)
		}
		combined.TablesToCreate = append(combined.TablesToCreate, p.TablesToCreate...)
		combined.ColumnsToAdd = append(combined.ColumnsToAdd, p.ColumnsToAdd...)
		combined.IndexesToCreate = append(combined.IndexesToCreate, p.IndexesToCreate...)
		combined.SQL = append(combined.SQL, p.SQL...)
	}
	combined.IsEmpty = len(combined.SQL) == 0
	return combined, nil
}

// Run applies all pending schema changes inside a single transaction.
// If any statement fails the whole transaction is rolled back.
func (m *Migrator) Run(ctx context.Context, models ...interface{}) error {
	// Ensure history table exists (best-effort; ignore error for fresh DBs)
	_ = ensureHistoryTable(ctx, m.db)

	plan, err := m.Plan(ctx, models...)
	if err != nil {
		return err
	}
	if plan.IsEmpty {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration: begin tx: %w", err)
	}

	for _, stmt := range plan.SQL {
		if strings.TrimSpace(stmt) == "" || strings.HasPrefix(strings.TrimSpace(stmt), "--") {
			continue
		}
		if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration: exec %q: %w", truncate(stmt, 80), execErr)
		}
	}

	// Record combined migration in history
	allSQL := strings.Join(plan.SQL, ";\n")
	checksum := fmt.Sprintf("%x", md5.Sum([]byte(allSQL)))
	version := fmt.Sprintf("auto_%s", checksum[:8])
	if err := recordMigration(ctx, tx, version, allSQL, checksum); err != nil {
		// Non-fatal: history might fail if dialect doesn't support AUTOINCREMENT
		_ = err
	}

	return tx.Commit()
}

// RunSQL executes arbitrary SQL statements as a migration.
// Useful for seeding or custom DDL that AutoMigrate cannot express.
func (m *Migrator) RunSQL(ctx context.Context, statements ...string) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, stmt := range statements {
		if _, e := tx.ExecContext(ctx, stmt); e != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration.RunSQL: %w (sql: %s)", e, truncate(stmt, 80))
		}
	}
	return tx.Commit()
}

// DropTable drops a table (dangerous — use with care).
// Only available for use in integration tests or dev environments.
func (m *Migrator) DropTable(ctx context.Context, table string) error {
	_, err := m.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s",
		m.dialect.QuoteIdent(table)))
	return err
}

// HasTable reports whether the given table exists in the database.
func (m *Migrator) HasTable(ctx context.Context, table string) (bool, error) {
	rows, err := m.db.QueryContext(ctx, m.dialect.TableExistsSQL(table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), nil
}

// HasColumn reports whether the given column exists in table.
func (m *Migrator) HasColumn(ctx context.Context, table, column string) (bool, error) {
	cols, err := currentColumns(ctx, m.db, m.dialect, table)
	if err != nil {
		return false, err
	}
	if cols == nil {
		return false, nil
	}
	_, ok := cols[strings.ToLower(column)]
	return ok, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
