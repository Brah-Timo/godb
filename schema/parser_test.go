package schema_test

import (
	"testing"
	"time"

	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  Test fixtures
// ─────────────────────────────────────────────────────────────

type User struct {
	ID        uint   `godb:"primary_key;auto_increment"`
	Name      string `godb:"not_null;size:100"`
	Email     string `godb:"unique;not_null;size:255"`
	Age       int    `godb:"index"`
	Active    bool   `godb:"default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time `godb:"softDelete"`
}

type Post struct {
	ID      uint   `godb:"primary_key;auto_increment"`
	UserID  uint   `godb:"not_null;index"`
	Title   string `godb:"not_null;size:500"`
	Content string
}

type CustomTable struct {
	ID int `godb:"primary_key"`
}

func (CustomTable) TableName() string { return "my_custom_table" }

// ─────────────────────────────────────────────────────────────
//  Tests
// ─────────────────────────────────────────────────────────────

func TestParseStruct_BasicModel(t *testing.T) {
	r := schema.NewRegistry()
	m, err := r.Parse(&User{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Table != "users" {
		t.Errorf("table: got %q, want %q", m.Table, "users")
	}
	if m.Name != "User" {
		t.Errorf("name: got %q, want %q", m.Name, "User")
	}
	if !m.HasSoftDelete {
		t.Error("expected HasSoftDelete = true")
	}
	if !m.HasAutoCreateTime {
		t.Error("expected HasAutoCreateTime = true")
	}
	if !m.HasAutoUpdateTime {
		t.Error("expected HasAutoUpdateTime = true")
	}
	if len(m.PrimaryKeys) != 1 {
		t.Errorf("primary keys: got %d, want 1", len(m.PrimaryKeys))
	}
	if m.PrimaryKeys[0].Column != "id" {
		t.Errorf("pk column: got %q, want %q", m.PrimaryKeys[0].Column, "id")
	}
}

func TestParseStruct_FieldAttributes(t *testing.T) {
	r := schema.NewRegistry()
	m, err := r.Parse(&User{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	email := m.ByColumn["email"]
	if email == nil {
		t.Fatal("email field not found")
	}
	if !email.Unique {
		t.Error("email: expected Unique = true")
	}
	if !email.NotNull {
		t.Error("email: expected NotNull = true")
	}
	if email.Size != 255 {
		t.Errorf("email size: got %d, want 255", email.Size)
	}

	age := m.ByColumn["age"]
	if age == nil {
		t.Fatal("age field not found")
	}
	if !age.HasIndex {
		t.Error("age: expected HasIndex = true")
	}
}

func TestParseStruct_CustomTableName(t *testing.T) {
	r := schema.NewRegistry()
	m, err := r.Parse(&CustomTable{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Table != "my_custom_table" {
		t.Errorf("table: got %q, want %q", m.Table, "my_custom_table")
	}
}

func TestRegistry_Caching(t *testing.T) {
	r := schema.NewRegistry()

	m1, err := r.Parse(&User{})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := r.Parse(&User{})
	if err != nil {
		t.Fatal(err)
	}
	// Must be the exact same pointer (cached)
	if m1 != m2 {
		t.Error("expected registry to return the same *Model pointer on second call")
	}
}

func TestRegistry_SliceInput(t *testing.T) {
	r := schema.NewRegistry()
	var users []User
	m, err := r.Parse(&users)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Table != "users" {
		t.Errorf("table from slice: got %q", m.Table)
	}
}

func TestToSnakeCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"UserName", "user_name"},
		{"ID", "id"},
		{"HTTPSProxy", "https_proxy"},
		{"CreatedAt", "created_at"},
		{"URLParser", "url_parser"},
		{"A", "a"},
		{"simplecase", "simplecase"},
	}
	for _, tc := range cases {
		got := schema.ToSnakeCase(tc.in)
		if got != tc.want {
			t.Errorf("ToSnakeCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────
//  Benchmarks
// ─────────────────────────────────────────────────────────────

func BenchmarkRegistryParse_Cached(b *testing.B) {
	r := schema.NewRegistry()
	// Warm up
	if _, err := r.Parse(&User{}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := r.Parse(&User{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRegistryParse_Cold(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := schema.NewRegistry()
		if _, err := r.Parse(&User{}); err != nil {
			b.Fatal(err)
		}
	}
}
