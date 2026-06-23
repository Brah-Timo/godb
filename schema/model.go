package schema

import (
	"fmt"
	"reflect"
)

// ─────────────────────────────────────────────────────────────
//  Model
// ─────────────────────────────────────────────────────────────

// Model is the fully-parsed metadata representation of a Go struct.
// It is created exactly once per unique reflect.Type and cached in the Registry.
type Model struct {
	// ── Identity ────────────────────────────────────────────
	Name  string       // Go struct name: "User"
	Table string       // SQL table name: "users"
	Type  reflect.Type // the concrete struct type (not pointer)

	// ── Fields ──────────────────────────────────────────────
	Fields      []*Field // all non-ignored fields in struct order
	PrimaryKeys []*Field // subset of Fields that are primary keys
	Relations   []*Field // subset of Fields that are relations (not mapped to a column)

	// Fast-access maps (O(1) lookup)
	ByGoName map[string]*Field // keyed by Go field name: "UserName"
	ByColumn map[string]*Field // keyed by DB column name: "user_name"

	// ── Capability flags (pre-computed) ─────────────────────
	HasSoftDelete     bool
	HasAutoCreateTime bool
	HasAutoUpdateTime bool

	// Hooks detected via interface satisfaction at parse time
	HasBeforeCreate bool
	HasAfterCreate  bool
	HasBeforeUpdate bool
	HasAfterUpdate  bool
	HasBeforeDelete bool
	HasAfterDelete  bool
	HasValidate     bool
}

// String returns a human-readable summary.
func (m *Model) String() string {
	return fmt.Sprintf("Model{Name:%s, Table:%s, Fields:%d}", m.Name, m.Table, len(m.Fields))
}

// Column returns the Field for the given column name, or nil.
func (m *Model) Column(col string) *Field {
	return m.ByColumn[col]
}

// Field returns the Field for the given Go field name, or nil.
func (m *Model) GoField(name string) *Field {
	return m.ByGoName[name]
}

// AutoTimeFields returns only the AutoCreateTime / AutoUpdateTime fields.
func (m *Model) AutoTimeFields() []*Field {
	var out []*Field
	for _, f := range m.Fields {
		if f.AutoCreateTime || f.AutoUpdateTime {
			out = append(out, f)
		}
	}
	return out
}

// ColumnFields returns fields that map to a real DB column (excludes relations).
func (m *Model) ColumnFields() []*Field {
	var out []*Field
	for _, f := range m.Fields {
		if f.Kind != KindRelation {
			out = append(out, f)
		}
	}
	return out
}

// WritableFields returns column fields that should be written on INSERT/UPDATE.
// Excludes auto-increment primary keys (they are DB-generated).
func (m *Model) WritableFields(op Operation) []*Field {
	var out []*Field
	for _, f := range m.ColumnFields() {
		if f.PrimaryKey && f.AutoIncrement {
			continue
		}
		if f.AutoCreateTime && op == OpUpdate {
			continue // don't overwrite created_at on update
		}
		out = append(out, f)
	}
	return out
}

// Operation is used by WritableFields to filter fields per operation type.
type Operation uint8

const (
	OpCreate Operation = iota
	OpUpdate
)

// ─────────────────────────────────────────────────────────────
//  Hook interfaces (detected at parse time)
// ─────────────────────────────────────────────────────────────

// The following interfaces can be implemented on your model structs.
// godb checks for them once at schema-parse time using reflect.Type.Implements().

type (
	// BeforeCreater is called before every INSERT.
	BeforeCreater interface{ BeforeCreate() error }
	// AfterCreater is called after a successful INSERT.
	AfterCreater interface{ AfterCreate() }
	// BeforeUpdater is called before every UPDATE.
	BeforeUpdater interface{ BeforeUpdate() error }
	// AfterUpdater is called after a successful UPDATE.
	AfterUpdater interface{ AfterUpdate() }
	// BeforeDeleter is called before every DELETE / soft-delete.
	BeforeDeleter interface{ BeforeDelete() error }
	// AfterDeleter is called after a successful DELETE / soft-delete.
	AfterDeleter interface{ AfterDelete() }
	// Validator is called before any write operation.
	Validator interface{ Validate() error }
)

var (
	beforeCreaterType = reflect.TypeOf((*BeforeCreater)(nil)).Elem()
	afterCreaterType  = reflect.TypeOf((*AfterCreater)(nil)).Elem()
	beforeUpdaterType = reflect.TypeOf((*BeforeUpdater)(nil)).Elem()
	afterUpdaterType  = reflect.TypeOf((*AfterUpdater)(nil)).Elem()
	beforeDeleterType = reflect.TypeOf((*BeforeDeleter)(nil)).Elem()
	afterDeleterType  = reflect.TypeOf((*AfterDeleter)(nil)).Elem()
	validatorType     = reflect.TypeOf((*Validator)(nil)).Elem()
)

// detectHooks sets the Has* flags on the model based on interface satisfaction.
// We check both T and *T because methods are usually defined on pointers.
func detectHooks(m *Model, t reflect.Type) {
	pt := reflect.PtrTo(t)
	check := func(iface reflect.Type) bool {
		return t.Implements(iface) || pt.Implements(iface)
	}
	m.HasBeforeCreate = check(beforeCreaterType)
	m.HasAfterCreate = check(afterCreaterType)
	m.HasBeforeUpdate = check(beforeUpdaterType)
	m.HasAfterUpdate = check(afterUpdaterType)
	m.HasBeforeDelete = check(beforeDeleterType)
	m.HasAfterDelete = check(afterDeleterType)
	m.HasValidate = check(validatorType)
}
