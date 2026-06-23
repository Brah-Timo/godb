package postgres_test

import (
	"testing"

	"github.com/Brah-Timo/godb/dialects/postgres"
	"github.com/Brah-Timo/godb/schema"
)

var d = &postgres.PostgresDialect{}

func TestReplacePlaceholders(t *testing.T) {
	cases := []struct {
		in      string
		startAt int
		want    string
	}{
		{"SELECT * FROM users WHERE id = ?", 1, "SELECT * FROM users WHERE id = $1"},
		{"a = ? AND b = ? AND c = ?", 1, "a = $1 AND b = $2 AND c = $3"},
		{"a = ? AND b = ?", 3, "a = $3 AND b = $4"},
		{"no placeholders", 1, "no placeholders"},
	}
	for _, tc := range cases {
		got := d.ReplacePlaceholders(tc.in, tc.startAt)
		if got != tc.want {
			t.Errorf("ReplacePlaceholders(%q, %d) = %q, want %q", tc.in, tc.startAt, got, tc.want)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"users", `"users"`},
		{`say"hello`, `"say""hello"`},
		{"user_name", `"user_name"`},
	}
	for _, tc := range cases {
		got := d.QuoteIdent(tc.in)
		if got != tc.want {
			t.Errorf("QuoteIdent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDataTypeOf(t *testing.T) {
	cases := []struct {
		field *schema.Field
		want  string
	}{
		{&schema.Field{Kind: schema.KindInt, AutoIncrement: true}, "BIGSERIAL"},
		{&schema.Field{Kind: schema.KindInt}, "BIGINT"},
		{&schema.Field{Kind: schema.KindString, Size: 255}, "VARCHAR(255)"},
		{&schema.Field{Kind: schema.KindString}, "TEXT"},
		{&schema.Field{Kind: schema.KindBool}, "BOOLEAN"},
		{&schema.Field{Kind: schema.KindTime}, "TIMESTAMPTZ"},
		{&schema.Field{Kind: schema.KindBytes}, "BYTEA"},
		{&schema.Field{Kind: schema.KindJSON}, "JSONB"},
		{&schema.Field{Kind: schema.KindFloat}, "DOUBLE PRECISION"},
		{&schema.Field{Kind: schema.KindFloat, Precision: 10, Scale: 2}, "NUMERIC(10,2)"},
		{&schema.Field{DBType: "CIDR"}, "CIDR"},
	}
	for _, tc := range cases {
		got := d.DataTypeOf(tc.field)
		if got != tc.want {
			t.Errorf("DataTypeOf(%+v) = %q, want %q", tc.field, got, tc.want)
		}
	}
}

func TestSupportsReturning(t *testing.T) {
	if !d.SupportsReturning() {
		t.Error("PostgreSQL must support RETURNING")
	}
}

func TestOnConflictDoUpdate(t *testing.T) {
	got := d.OnConflictDoUpdate([]string{"id"}, []string{"name", "email"})
	want := `ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name", "email" = EXCLUDED."email"`
	if got != want {
		t.Errorf("OnConflictDoUpdate:\ngot  %q\nwant %q", got, want)
	}
}

func BenchmarkReplacePlaceholders(b *testing.B) {
	sql := "SELECT * FROM users WHERE id = ? AND name = ? AND age > ?"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d.ReplacePlaceholders(sql, 1)
	}
}
