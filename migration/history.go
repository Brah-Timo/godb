package migration

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// historyTable is the table godb uses to record which migrations have been applied.
const historyTable = "godb_migrations"

// MigrationRecord represents one entry in the migration history.
type MigrationRecord struct {
	ID        int64
	Version   string
	AppliedAt time.Time
	SQL       string
	Checksum  string
}

// ensureHistoryTable creates the godb_migrations table if it does not exist.
func ensureHistoryTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    id         INTEGER     PRIMARY KEY AUTOINCREMENT,
    version    TEXT        NOT NULL UNIQUE,
    applied_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sql_text   TEXT        NOT NULL,
    checksum   TEXT        NOT NULL
)`, historyTable))
	return err
}

// recordMigration inserts a migration record into the history table.
func recordMigration(ctx context.Context, tx *sql.Tx, version, sqlText, checksum string) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (version, sql_text, checksum) VALUES (?, ?, ?)`,
		historyTable), version, sqlText, checksum)
	return err
}

// appliedVersions returns the set of migration versions already in the history.
func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT version FROM %s", historyTable))
	if err != nil {
		// Table might not exist yet
		return map[string]bool{}, nil
	}
	defer rows.Close()
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}
