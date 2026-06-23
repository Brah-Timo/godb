// Package schema parses Go struct types into a rich metadata model that the
// builder, migrator, and relation loader use to generate SQL.
//
// Parsing happens exactly once per unique Go type, after which the result is
// stored in the Registry (a concurrent-safe cache), so repeated calls are O(1).
package schema

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ─────────────────────────────────────────────────────────────
//  FieldKind — the semantic kind of a struct field
// ─────────────────────────────────────────────────────────────

// FieldKind classifies every field into one of the categories below.
// The dialect uses this to decide the SQL column type when none is explicit.
type FieldKind uint8

const (
	KindInvalid  FieldKind = iota
	KindString             // string, []byte (text)
	KindInt                // int, int8, int16, int32, int64, uint*
	KindFloat              // float32, float64
	KindBool               // bool
	KindTime               // time.Time, *time.Time
	KindBytes              // []byte (binary blob)
	KindJSON               // map / interface{} / any struct with json tag
	KindRelation           // a field referencing another godb model
)

// ─────────────────────────────────────────────────────────────
//  Relation metadata
// ─────────────────────────────────────────────────────────────

// RelationType classifies the cardinality of a model relationship.
type RelationType uint8

const (
	RelHasOne     RelationType = iota // 1-to-1, parent owns child
	RelHasMany                        // 1-to-many, parent owns children
	RelBelongsTo                      // N-to-1, child owns parent FK
	RelManyToMany                     // M-to-N via join table
)

// Relation holds all information needed to load a relationship.
type Relation struct {
	Type       RelationType
	Model      string // referenced Go struct name
	ForeignKey string // column name (e.g. "user_id")
	References string // column in parent (default: primary key)
	JoinTable  string // for M:N
	JoinFK     string // FK on join table pointing to this model
	JoinRefFK  string // FK on join table pointing to referenced model
}

// ─────────────────────────────────────────────────────────────
//  Field
// ─────────────────────────────────────────────────────────────

// Field represents a single struct field with all its metadata.
type Field struct {
	// ── Go-side ─────────────────────────────────────────────
	Name      string       // exported struct field name: "UserName"
	GoType    reflect.Type // the concrete Go type
	StructIdx []int        // reflect.Value.FieldByIndex path (supports embedded)
	Kind      FieldKind
	IsPtr     bool // true if GoType is *T

	// ── DB-side ─────────────────────────────────────────────
	Column string // SQL column name: "user_name"
	DBType string // explicit SQL type: "VARCHAR(255)" — empty = dialect decides

	// ── Constraints & attributes ────────────────────────────
	PrimaryKey    bool
	AutoIncrement bool
	NotNull       bool
	Unique        bool
	HasIndex      bool   // CREATE INDEX
	Default       string // SQL DEFAULT expression
	Size          int    // VARCHAR(Size) or DECIMAL(Size,Scale)
	Scale         int    // DECIMAL scale
	Precision     int    // DECIMAL precision

	// ── Automatic timestamps ────────────────────────────────
	AutoCreateTime bool // filled on INSERT
	AutoUpdateTime bool // filled on INSERT + UPDATE
	SoftDelete     bool // filled on soft-delete UPDATE

	// ── Advanced ────────────────────────────────────────────
	Encrypted bool // paid feature: field is AES-GCM-encrypted at rest
	Ignored   bool // godb:"-"

	// ── Relations ───────────────────────────────────────────
	Relation *Relation
}

// String returns a human-readable summary of the field (for debug/logs).
func (f *Field) String() string {
	return fmt.Sprintf("Field{Go:%s, Column:%s, Kind:%d, PK:%v}", f.Name, f.Column, f.Kind, f.PrimaryKey)
}

// ─────────────────────────────────────────────────────────────
//  Tag parsing
// ─────────────────────────────────────────────────────────────

const tagName = "godb"

// parseTag reads the `godb:"..."` struct tag and applies it to f.
//
// Tag grammar (semicolon-separated, colon-separated key:value):
//
//	godb:"column:col_name;type:varchar(255);primary_key;auto_increment;not_null;
//	       unique;index;default:now();size:255;precision:10;scale:2;
//	       autoCreateTime;autoUpdateTime;softDelete;encrypted;-"
//
//	Relation tags:
//	  has_one;has_many;belongs_to;many_to_many
//	  foreign_key:user_id;references:id;join_table:post_tags
func (f *Field) parseTag(tag string) {
	if tag == "" {
		return
	}
	if tag == "-" {
		f.Ignored = true
		return
	}

	parts := strings.Split(tag, ";")
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kv := strings.SplitN(raw, ":", 2)
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := ""
		if len(kv) == 2 {
			val = strings.TrimSpace(kv[1])
		}

		switch key {
		case "-":
			f.Ignored = true
		case "column":
			f.Column = val
		case "type":
			f.DBType = val
		case "primary_key", "primarykey":
			f.PrimaryKey = true
		case "auto_increment", "autoincrement":
			f.AutoIncrement = true
		case "not_null", "notnull":
			f.NotNull = true
		case "unique":
			f.Unique = true
		case "index":
			f.HasIndex = true
		case "default":
			f.Default = val
		case "size":
			f.Size, _ = strconv.Atoi(val)
		case "precision":
			f.Precision, _ = strconv.Atoi(val)
		case "scale":
			f.Scale, _ = strconv.Atoi(val)
		case "autocreatetime":
			f.AutoCreateTime = true
		case "autoupdatetime":
			f.AutoUpdateTime = true
		case "softdelete":
			f.SoftDelete = true
		case "encrypted":
			f.Encrypted = true

		// ── Relation tags ────────────────────────────────────
		case "has_one":
			f.Kind = KindRelation
			if f.Relation == nil {
				f.Relation = &Relation{Type: RelHasOne}
			} else {
				f.Relation.Type = RelHasOne
			}
		case "has_many":
			f.Kind = KindRelation
			if f.Relation == nil {
				f.Relation = &Relation{Type: RelHasMany}
			} else {
				f.Relation.Type = RelHasMany
			}
		case "belongs_to":
			f.Kind = KindRelation
			if f.Relation == nil {
				f.Relation = &Relation{Type: RelBelongsTo}
			} else {
				f.Relation.Type = RelBelongsTo
			}
		case "many_to_many":
			f.Kind = KindRelation
			if f.Relation == nil {
				f.Relation = &Relation{Type: RelManyToMany}
			} else {
				f.Relation.Type = RelManyToMany
			}
		case "foreign_key":
			f.ensureRelation().ForeignKey = val
		case "references":
			f.ensureRelation().References = val
		case "join_table":
			f.ensureRelation().JoinTable = val
		}
	}
}

func (f *Field) ensureRelation() *Relation {
	if f.Relation == nil {
		f.Relation = &Relation{}
	}
	return f.Relation
}

// ─────────────────────────────────────────────────────────────
//  Kind inference from Go type
// ─────────────────────────────────────────────────────────────

var (
	timeType  = reflect.TypeOf((*interface{ IsZero() bool })(nil)).Elem() // time.Time duck
	byteSlice = reflect.TypeOf([]byte(nil))
)

// inferKind deduces the FieldKind from the Go type when no explicit DB type is set.
func inferKind(t reflect.Type) FieldKind {
	// Dereference pointer
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return KindString
	case reflect.Bool:
		return KindBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr:
		return KindInt
	case reflect.Float32, reflect.Float64:
		return KindFloat
	case reflect.Slice:
		if t == byteSlice {
			return KindBytes
		}
		return KindRelation // slice of structs → has_many
	case reflect.Struct:
		// Check for time.Time (duck-type check for IsZero())
		if t.Implements(timeType) {
			return KindTime
		}
		if m, ok := t.MethodByName("IsZero"); ok && m.Type.NumOut() == 1 {
			return KindTime
		}
		return KindRelation
	case reflect.Ptr:
		return inferKind(t.Elem())
	case reflect.Map, reflect.Interface:
		return KindJSON
	}
	return KindInvalid
}
