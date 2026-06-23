package migration_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/Brah-Timo/godb/dialects"
	_ "github.com/Brah-Timo/godb/dialects/sqlite"
	"github.com/Brah-Timo/godb/migration"
	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  Test models
// ─────────────────────────────────────────────────────────────

type MigUser struct {
	ID        int64  `godb:"primary_key;auto_increment"`
	Name      string `godb:"not_null;size:100"`
	Email     string `godb:"unique;not_null;size:255"`
	Age       int
	CreatedAt time.Time
	DeletedAt *time.Time `godb:"softDelete"`
}

type MigPost struct {
	ID     int64  `godb:"primary_key;auto_increment"`
	Title  string `godb:"not_null;size:255"`
	UserID int64  `godb:"index"`
}

// ─────────────────────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────────────────────

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite3: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestMigrator(t *testing.T, db *sql.DB) *migration.Migrator {
	t.Helper()
	d, err := dialects.Get("sqlite3")
	if err != nil {
		t.Fatalf("dialects.Get: %v", err)
	}
	reg := schema.NewRegistry()
	return migration.NewMigrator(db, d, reg)
}

// ─────────────────────────────────────────────────────────────
//  Tests
// ─────────────────────────────────────────────────────────────

func TestMigrator_Plan_NewTable(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	plan, err := m.Plan(context.Background(), &MigUser{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if plan.IsEmpty {
		t.Error("expected non-empty plan for new table")
	}
	if len(plan.TablesToCreate) == 0 {
		t.Error("expected at least one table to create")
	}
	t.Logf("Plan SQL:\n%v", plan.SQL)
}

func TestMigrator_Run_CreateTable(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	if err := m.Run(context.Background(), &MigUser{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify table exists
	exists, err := m.HasTable(context.Background(), "mig_users")
	if err != nil {
		t.Fatalf("HasTable: %v", err)
	}
	if !exists {
		t.Error("expected table 'mig_users' to exist after migration")
	}
}

func TestMigrator_Run_MultipleModels(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	if err := m.Run(context.Background(), &MigUser{}, &MigPost{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, table := range []string{"mig_users", "mig_posts"} {
		exists, err := m.HasTable(context.Background(), table)
		if err != nil {
			t.Fatalf("HasTable(%s): %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist", table)
		}
	}
}

func TestMigrator_Run_Idempotent(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	// First migration
	if err := m.Run(context.Background(), &MigUser{}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Second migration — should be no-op
	if err := m.Run(context.Background(), &MigUser{}); err != nil {
		t.Fatalf("second Run (idempotent): %v", err)
	}
}

func TestMigrator_HasColumn(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	if err := m.Run(context.Background(), &MigUser{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	tests := []struct {
		col    string
		exists bool
	}{
		{"name", true},
		{"email", true},
		{"age", true},
		{"nonexistent_column", false},
	}
	for _, tt := range tests {
		got, err := m.HasColumn(context.Background(), "mig_users", tt.col)
		if err != nil {
			t.Errorf("HasColumn(%s): %v", tt.col, err)
			continue
		}
		if got != tt.exists {
			t.Errorf("HasColumn(%s) = %v, want %v", tt.col, got, tt.exists)
		}
	}
}

func TestMigrator_RunSQL(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	// Create table manually via RunSQL
	err := m.RunSQL(context.Background(),
		"CREATE TABLE IF NOT EXISTS custom_table (id INTEGER PRIMARY KEY, val TEXT)",
	)
	if err != nil {
		t.Fatalf("RunSQL: %v", err)
	}

	exists, err := m.HasTable(context.Background(), "custom_table")
	if err != nil {
		t.Fatalf("HasTable: %v", err)
	}
	if !exists {
		t.Error("expected 'custom_table' to exist after RunSQL")
	}
}

func TestMigrator_DropTable(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	// Create table first
	if err := m.Run(context.Background(), &MigPost{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Drop it
	if err := m.DropTable(context.Background(), "mig_posts"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}

	exists, _ := m.HasTable(context.Background(), "mig_posts")
	if exists {
		t.Error("expected 'mig_posts' to be gone after DropTable")
	}
}

func TestMigrator_Plan_AlreadyExists(t *testing.T) {
	db := newTestDB(t)
	m := newTestMigrator(t, db)

	// Create table first
	if err := m.Run(context.Background(), &MigUser{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Plan after creation should be empty
	plan, err := m.Plan(context.Background(), &MigUser{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.IsEmpty {
		t.Errorf("expected empty plan for existing table, got SQL: %v", plan.SQL)
	}
}

// ─────────────────────────────────────────────────────────────
//  Benchmarks
// ─────────────────────────────────────────────────────────────

func BenchmarkMigrator_Plan(b *testing.B) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer db.Close()

	d, _ := dialects.Get("sqlite3")
	reg := schema.NewRegistry()
	m := migration.NewMigrator(db, d, reg)

	// Pre-create the table so Plan is an O(0) diff
	_ = m.Run(context.Background(), &MigUser{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Plan(context.Background(), &MigUser{})
	}
}
