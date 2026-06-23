package builder_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Brah-Timo/godb/builder"
	"github.com/Brah-Timo/godb/cache"
	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  Test helpers
// ─────────────────────────────────────────────────────────────

// mockDialect is a minimal Dialect implementation for testing (uses ? placeholders).
type mockDialect struct{}

func (mockDialect) Name() string                                     { return "mock" }
func (mockDialect) ReplacePlaceholders(sql string, _ int) string     { return sql }
func (mockDialect) QuoteIdent(name string) string                    { return `"` + name + `"` }
func (mockDialect) DataTypeOf(_ *schema.Field) string                { return "TEXT" }
func (mockDialect) CreateTableSQL(_ *schema.Model) string            { return "" }
func (mockDialect) AddColumnSQL(_ string, _ *schema.Field) string    { return "" }
func (mockDialect) ModifyColumnSQL(_ string, _ *schema.Field) string { return "" }
func (mockDialect) DropColumnSQL(_, _ string) string                 { return "" }
func (mockDialect) CreateIndexSQL(_ string, _ *schema.Field) string  { return "" }
func (mockDialect) TableExistsSQL(_ string) string                   { return "" }
func (mockDialect) ColumnsSQL(_ string) string                       { return "" }
func (mockDialect) IndexesSQL(_ string) string                       { return "" }
func (mockDialect) SupportsReturning() bool                          { return false }
func (mockDialect) OnConflictDoUpdate(_, _ []string) string          { return "" }
func (mockDialect) LimitOffset(limit, offset int) string {
	if limit > 0 && offset > 0 {
		return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
	}
	if limit > 0 {
		return fmt.Sprintf("LIMIT %d", limit)
	}
	return ""
}

// compile-time check
var _ dialects.Dialect = mockDialect{}

// testUser is the struct used by builder tests.
type testUser struct {
	ID    uint   `godb:"primary_key;auto_increment"`
	Name  string `godb:"not_null;size:100"`
	Email string `godb:"unique;not_null;size:255"`
	Age   int
}

// newTestChain returns a Chain wired to mockDialect and a parsed testUser model.
func newTestChain(t *testing.T) *builder.Chain {
	t.Helper()
	d := mockDialect{}
	reg := schema.NewRegistry()
	m, err := reg.Parse(&testUser{})
	if err != nil {
		t.Fatalf("registry.Parse: %v", err)
	}
	return builder.New(
		nil, // no db needed for SQL-generation tests
		d,
		m,
		cache.NewNop(),
		nil,
		nil,
		context.Background(),
		false,
		nil,
	)
}

// ─────────────────────────────────────────────────────────────
//  ToSQL tests
// ─────────────────────────────────────────────────────────────

func TestChain_ToSQL_Select(t *testing.T) {
	c := newTestChain(t)
	sql, args := c.ToSQL()

	if sql == "" {
		t.Error("ToSQL returned empty SQL")
	}
	if args == nil {
		args = []interface{}{}
	}
	t.Logf("SQL: %s  ARGS: %v", sql, args)
}

func TestChain_ToSQL_Where(t *testing.T) {
	c := newTestChain(t).Where("age > ?", 18)
	sql, args := c.ToSQL()

	if sql == "" {
		t.Fatal("ToSQL returned empty SQL")
	}
	if len(args) != 1 || args[0] != 18 {
		t.Errorf("args = %v, want [18]", args)
	}
	t.Logf("SQL: %s  ARGS: %v", sql, args)
}

func TestChain_ToSQL_WhereMultiple(t *testing.T) {
	c := newTestChain(t).
		Where("age > ?", 18).
		Where("name LIKE ?", "%alice%")
	sql, args := c.ToSQL()
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d: %v", len(args), args)
	}
	t.Logf("SQL: %s  ARGS: %v", sql, args)
}

func TestChain_ToSQL_OrderLimitOffset(t *testing.T) {
	c := newTestChain(t).Order("name ASC").Limit(10).Offset(20)
	sql, _ := c.ToSQL()
	if sql == "" {
		t.Error("ToSQL returned empty SQL")
	}
	t.Logf("SQL: %s", sql)
}

func TestChain_ToSQL_Select_Columns(t *testing.T) {
	c := newTestChain(t).Select("id", "name")
	sql, _ := c.ToSQL()
	if sql == "" {
		t.Error("ToSQL returned empty SQL")
	}
	t.Logf("SQL: %s", sql)
}

func TestChain_ToSQL_Distinct(t *testing.T) {
	c := newTestChain(t).Distinct()
	sql, _ := c.ToSQL()
	t.Logf("Distinct SQL: %s", sql)
}

func TestChain_RawSQL(t *testing.T) {
	c := builder.NewTable(
		nil, mockDialect{}, "users",
		cache.NewNop(), nil, nil,
		context.Background(), false, nil,
	)
	rawSQL := "SELECT * FROM users WHERE id = ?"
	rawArgs := []interface{}{42}
	c = c.Raw(rawSQL, rawArgs...)
	sql, args := c.ToSQL()
	if sql != rawSQL {
		t.Errorf("ToSQL sql = %q, want %q", sql, rawSQL)
	}
	if len(args) != 1 || args[0] != 42 {
		t.Errorf("args = %v, want [42]", args)
	}
}

// ─────────────────────────────────────────────────────────────
//  Immutability tests
// ─────────────────────────────────────────────────────────────

func TestChain_Immutable(t *testing.T) {
	base := newTestChain(t)
	derived := base.Where("age > ?", 18)

	// base should NOT have the where clause
	basSQL, baseArgs := base.ToSQL()
	derSQL, derArgs := derived.ToSQL()

	if len(baseArgs) != 0 {
		t.Errorf("base chain was mutated: args = %v", baseArgs)
	}
	if len(derArgs) != 1 {
		t.Errorf("derived chain missing arg: %v", derArgs)
	}
	t.Logf("base SQL: %s", basSQL)
	t.Logf("derived SQL: %s", derSQL)
}

func TestChain_Immutable_MultipleClones(t *testing.T) {
	base := newTestChain(t)
	c1 := base.Where("a = ?", 1)
	c2 := base.Where("b = ?", 2)
	c3 := c1.Where("c = ?", 3)

	_, args1 := c1.ToSQL()
	_, args2 := c2.ToSQL()
	_, args3 := c3.ToSQL()

	if len(args1) != 1 {
		t.Errorf("c1 args = %v, want 1", args1)
	}
	if len(args2) != 1 {
		t.Errorf("c2 args = %v, want 1", args2)
	}
	if len(args3) != 2 {
		t.Errorf("c3 args = %v, want 2", args3)
	}
}

// ─────────────────────────────────────────────────────────────
//  WhereIn / WhereNotIn tests
// ─────────────────────────────────────────────────────────────

func TestChain_WhereIn(t *testing.T) {
	c := newTestChain(t).WhereIn("id", 1, 2, 3)
	_, args := c.ToSQL()
	if len(args) != 3 {
		t.Errorf("WhereIn args = %v, want 3", args)
	}
}

func TestChain_WhereIn_Empty(t *testing.T) {
	c := newTestChain(t).WhereIn("id")
	sql, _ := c.ToSQL()
	if sql == "" {
		t.Error("expected SQL even for empty WhereIn")
	}
	// Should produce "1 = 0" to return no rows
	t.Logf("WhereIn(empty) SQL: %s", sql)
}

func TestChain_WhereNotIn(t *testing.T) {
	c := newTestChain(t).WhereNotIn("id", 10, 20)
	_, args := c.ToSQL()
	if len(args) != 2 {
		t.Errorf("WhereNotIn args = %v, want 2", args)
	}
}

// ─────────────────────────────────────────────────────────────
//  Join tests
// ─────────────────────────────────────────────────────────────

func TestChain_InnerJoin(t *testing.T) {
	c := newTestChain(t).Joins("posts", "posts.user_id = users.id")
	sql, _ := c.ToSQL()
	t.Logf("JOIN SQL: %s", sql)
	if sql == "" {
		t.Error("expected SQL with JOIN")
	}
}

func TestChain_LeftJoin(t *testing.T) {
	c := newTestChain(t).LeftJoin("posts", "posts.user_id = users.id")
	sql, _ := c.ToSQL()
	t.Logf("LEFT JOIN SQL: %s", sql)
}

// ─────────────────────────────────────────────────────────────
//  GroupBy / Having tests
// ─────────────────────────────────────────────────────────────

func TestChain_GroupByHaving(t *testing.T) {
	c := newTestChain(t).GroupBy("age").Having("COUNT(*) > ?", 5)
	sql, args := c.ToSQL()
	if len(args) != 1 {
		t.Errorf("Having args = %v, want 1", args)
	}
	t.Logf("GROUP BY SQL: %s  ARGS: %v", sql, args)
}

// ─────────────────────────────────────────────────────────────
//  Cache / DryRun settings
// ─────────────────────────────────────────────────────────────

func TestChain_CacheTTL(t *testing.T) {
	ttl := 5 * time.Minute
	c := newTestChain(t).Cache(ttl)
	sql, _ := c.ToSQL()
	if sql == "" {
		t.Error("expected non-empty SQL after Cache()")
	}
}

// ─────────────────────────────────────────────────────────────
//  NewErr chain
// ─────────────────────────────────────────────────────────────

func TestChain_NewErr(t *testing.T) {
	c := builder.NewErr(fmt.Errorf("test error"))
	var dest []testUser
	err := c.Find(&dest)
	if err == nil {
		t.Error("expected error from NewErr chain")
	}
	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %q", err.Error())
	}
}

// ─────────────────────────────────────────────────────────────
//  Benchmarks
// ─────────────────────────────────────────────────────────────

func BenchmarkChain_ToSQL_Simple(b *testing.B) {
	reg := schema.NewRegistry()
	m, _ := reg.Parse(&testUser{})
	d := mockDialect{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := builder.New(nil, d, m, nil, nil, nil, context.Background(), false, nil)
		c = c.Where("age > ?", 18).Order("name ASC").Limit(10)
		_, _ = c.ToSQL()
	}
}

func BenchmarkChain_ToSQL_Complex(b *testing.B) {
	reg := schema.NewRegistry()
	m, _ := reg.Parse(&testUser{})
	d := mockDialect{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := builder.New(nil, d, m, nil, nil, nil, context.Background(), false, nil)
		c = c.
			Where("age > ?", 18).
			Where("name LIKE ?", "%a%").
			WhereIn("id", 1, 2, 3, 4, 5).
			Order("name ASC").
			GroupBy("age").
			Having("COUNT(*) > ?", 1).
			Limit(20).
			Offset(40)
		_, _ = c.ToSQL()
	}
}

func BenchmarkChain_Clone(b *testing.B) {
	reg := schema.NewRegistry()
	m, _ := reg.Parse(&testUser{})
	d := mockDialect{}
	base := builder.New(nil, d, m, nil, nil, nil, context.Background(), false, nil)
	base = base.Where("x = ?", 1).Where("y = ?", 2).Order("z")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = base.Where("z = ?", i)
	}
}
