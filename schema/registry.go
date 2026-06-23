package schema

import (
	"fmt"
	"reflect"
	"sync"
)

// ─────────────────────────────────────────────────────────────
//  Registry — the global schema cache
// ─────────────────────────────────────────────────────────────

// Registry stores parsed Model instances keyed by their reflect.Type.
// It is goroutine-safe: reads use RLock, writes use the full lock.
// Parse is the only public method you need; everything else is internal.
type Registry struct {
	mu      sync.RWMutex
	schemas map[reflect.Type]*Model
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		schemas: make(map[reflect.Type]*Model),
	}
}

// Parse returns the Model for the concrete struct type of value.
// value may be any of:
//   - *MyStruct
//   - MyStruct
//   - *[]MyStruct
//   - []MyStruct
//   - []*MyStruct
//
// The result is cached; subsequent calls for the same type are O(1) map lookups.
func (r *Registry) Parse(value interface{}) (*Model, error) {
	t, err := resolveType(value)
	if err != nil {
		return nil, err
	}

	// Fast path: check read-lock first
	r.mu.RLock()
	if m, ok := r.schemas[t]; ok {
		r.mu.RUnlock()
		return m, nil
	}
	r.mu.RUnlock()

	// Slow path: parse and store
	m, err := parseStruct(t)
	if err != nil {
		return nil, fmt.Errorf("schema.Parse(%s): %w", t.Name(), err)
	}

	r.mu.Lock()
	// Double-check: another goroutine may have won the race
	if existing, ok := r.schemas[t]; ok {
		r.mu.Unlock()
		return existing, nil
	}
	r.schemas[t] = m
	r.mu.Unlock()

	return m, nil
}

// ParseType is like Parse but accepts a reflect.Type directly.
func (r *Registry) ParseType(t reflect.Type) (*Model, error) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("schema.ParseType: expected struct, got %s", t.Kind())
	}

	r.mu.RLock()
	if m, ok := r.schemas[t]; ok {
		r.mu.RUnlock()
		return m, nil
	}
	r.mu.RUnlock()

	m, err := parseStruct(t)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if existing, ok := r.schemas[t]; ok {
		r.mu.Unlock()
		return existing, nil
	}
	r.schemas[t] = m
	r.mu.Unlock()
	return m, nil
}

// All returns all currently cached models (for introspection / debugging).
func (r *Registry) All() []*Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Model, 0, len(r.schemas))
	for _, m := range r.schemas {
		out = append(out, m)
	}
	return out
}

// Clear evicts all cached entries (useful in tests).
func (r *Registry) Clear() {
	r.mu.Lock()
	r.schemas = make(map[reflect.Type]*Model)
	r.mu.Unlock()
}

// ─────────────────────────────────────────────────────────────
//  Type resolution helpers
// ─────────────────────────────────────────────────────────────

// resolveType dereferences pointers and slices to arrive at the underlying
// struct reflect.Type.
func resolveType(value interface{}) (reflect.Type, error) {
	if value == nil {
		return nil, fmt.Errorf("schema: nil value passed to Parse")
	}
	t := reflect.TypeOf(value)
	return deref(t)
}

// deref unwraps Ptr and Slice kinds until it reaches a Struct.
func deref(t reflect.Type) (reflect.Type, error) {
	for {
		switch t.Kind() {
		case reflect.Ptr:
			t = t.Elem()
		case reflect.Slice, reflect.Array:
			t = t.Elem()
		case reflect.Struct:
			return t, nil
		default:
			return nil, fmt.Errorf("schema: cannot derive struct from %s", t.Kind())
		}
	}
}

// ─────────────────────────────────────────────────────────────
//  Value helpers used by the builder
// ─────────────────────────────────────────────────────────────

// FieldValue extracts the reflect.Value of field f from the struct v.
// Handles embedded structs via f.StructIdx.
func FieldValue(v reflect.Value, f *Field) reflect.Value {
	return v.FieldByIndex(f.StructIdx)
}

// SetField sets field f in struct pointer ptrValue to newVal.
func SetField(ptrValue reflect.Value, f *Field, newVal interface{}) {
	fv := ptrValue.Elem().FieldByIndex(f.StructIdx)
	if !fv.CanSet() {
		return
	}
	rv := reflect.ValueOf(newVal)
	if rv.Type().AssignableTo(fv.Type()) {
		fv.Set(rv)
	} else if rv.Type().ConvertibleTo(fv.Type()) {
		fv.Set(rv.Convert(fv.Type()))
	}
}

// IsZeroValue reports whether a reflect.Value is the zero value for its type.
func IsZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func, reflect.Map, reflect.Slice:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}
