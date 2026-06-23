// Package migration provides schema migration capabilities for godb.
//
// It compares the current database schema against the desired Go struct models
// and generates the minimal set of DDL statements needed to bring them in sync.
// It NEVER drops columns or tables — only adds new ones.
//
// Usage:
//
//	db.AutoMigrate(&User{}, &Post{}, &Tag{})
package migration

import (
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────────────────────
//  Plan
// ─────────────────────────────────────────────────────────────

// Plan represents the set of changes AutoMigrate would apply to the database.
// Inspect it before execution via DB.MigratePlan().
type Plan struct {
	// TablesToCreate contains names of tables that do not exist yet.
	TablesToCreate []string
	// ColumnsToAdd contains new columns to add to existing tables.
	ColumnsToAdd []ColumnDiff
	// IndexesToCreate contains new indexes to create.
	IndexesToCreate []IndexDiff
	// SQL contains the ordered list of DDL statements to execute.
	SQL []string
	// IsEmpty reports whether there are any changes.
	IsEmpty bool
}

// ColumnDiff describes a new column to be added.
type ColumnDiff struct {
	Table  string
	Column string
	Type   string
	SQL    string
}

// IndexDiff describes a new index to be created.
type IndexDiff struct {
	Table  string
	Column string
	Unique bool
	SQL    string
}

// Summary returns a human-readable summary of the plan.
func (p *Plan) Summary() string {
	if p.IsEmpty {
		return "No migrations needed."
	}
	var sb strings.Builder
	if len(p.TablesToCreate) > 0 {
		fmt.Fprintf(&sb, "CREATE TABLE: %s\n", strings.Join(p.TablesToCreate, ", "))
	}
	for _, c := range p.ColumnsToAdd {
		fmt.Fprintf(&sb, "ADD COLUMN: %s.%s (%s)\n", c.Table, c.Column, c.Type)
	}
	for _, idx := range p.IndexesToCreate {
		fmt.Fprintf(&sb, "CREATE INDEX: %s.%s (unique=%v)\n", idx.Table, idx.Column, idx.Unique)
	}
	return sb.String()
}
