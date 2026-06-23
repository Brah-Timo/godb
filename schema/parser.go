package schema

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

// ─────────────────────────────────────────────────────────────
//  TableNamer interface
// ─────────────────────────────────────────────────────────────

// TableNamer can be implemented by model structs to override the default
// snake_plural table name derived from the struct name.
//
//	func (User) TableName() string { return "app_users" }
type TableNamer interface {
	TableName() string
}

// ─────────────────────────────────────────────────────────────
//  parseStruct — the core reflection engine
// ─────────────────────────────────────────────────────────────

// parseStruct performs a full reflect traversal of t and returns a Model.
// t must be a non-pointer struct type.
// Called by Registry.Parse exactly once per unique type.
func parseStruct(t reflect.Type) (*Model, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("schema: expected struct, got %s", t.Kind())
	}

	m := &Model{
		Name:     t.Name(),
		Table:    toSnakePlural(t.Name()),
		Type:     t,
		ByGoName: make(map[string]*Field),
		ByColumn: make(map[string]*Field),
	}

	// Allow the struct to override the table name
	// We need a pointer receiver to call pointer-receiver methods.
	if v := reflect.New(t); v.CanInterface() {
		if tn, ok := v.Interface().(TableNamer); ok {
			m.Table = tn.TableName()
		}
	}

	if err := parseFields(m, t, nil); err != nil {
		return nil, err
	}

	detectHooks(m, t)
	return m, nil
}

// parseFields recursively processes struct fields (handles embedded structs).
func parseFields(m *Model, t reflect.Type, indexPrefix []int) error {
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		// Skip unexported fields
		if !sf.IsExported() {
			continue
		}

		// Handle embedded (anonymous) structs — flatten their fields
		if sf.Anonymous {
			ft := sf.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if err := parseFields(m, ft, append(indexPrefix, i)); err != nil {
					return err
				}
				continue
			}
		}

		field := buildField(sf, append(indexPrefix, i))

		if field.Ignored {
			continue
		}

		// Auto-detect well-known field names
		autoDetect(field, sf)

		// Register capability flags
		if field.PrimaryKey {
			m.PrimaryKeys = append(m.PrimaryKeys, field)
		}
		if field.SoftDelete {
			m.HasSoftDelete = true
		}
		if field.AutoCreateTime {
			m.HasAutoCreateTime = true
		}
		if field.AutoUpdateTime {
			m.HasAutoUpdateTime = true
		}
		if field.Kind == KindRelation {
			m.Relations = append(m.Relations, field)
		}

		m.Fields = append(m.Fields, field)
		m.ByGoName[field.Name] = field
		if field.Column != "" && field.Kind != KindRelation {
			if _, dup := m.ByColumn[field.Column]; dup {
				return fmt.Errorf("schema: duplicate column %q in model %s", field.Column, m.Name)
			}
			m.ByColumn[field.Column] = field
		}
	}
	return nil
}

// buildField constructs a Field from a reflect.StructField.
func buildField(sf reflect.StructField, idx []int) *Field {
	t := sf.Type
	isPtr := t.Kind() == reflect.Ptr
	if isPtr {
		t = t.Elem()
	}

	f := &Field{
		Name:      sf.Name,
		GoType:    sf.Type,
		StructIdx: idx,
		IsPtr:     isPtr,
		Column:    toSnakeCase(sf.Name),
		Kind:      inferKind(sf.Type),
	}

	// Parse `godb:"..."` tag
	if tag, ok := sf.Tag.Lookup(tagName); ok {
		f.parseTag(tag)
	}

	return f
}

// autoDetect applies convention-based detection for well-known field names
// when the user has not set explicit tags.
func autoDetect(f *Field, sf reflect.StructField) {
	name := strings.ToLower(sf.Name)

	// Primary key: field named "ID" or "Id" on a model → primary_key + auto_increment
	if (sf.Name == "ID" || sf.Name == "Id") && !f.PrimaryKey {
		f.PrimaryKey = true
		if f.Kind == KindInt {
			f.AutoIncrement = true
		}
	}

	// Timestamps
	switch name {
	case "createdat":
		if !f.AutoCreateTime && f.Kind == KindTime {
			f.AutoCreateTime = true
		}
	case "updatedat":
		if !f.AutoUpdateTime && f.Kind == KindTime {
			f.AutoUpdateTime = true
		}
	case "deletedat":
		if !f.SoftDelete && (f.Kind == KindTime || f.IsPtr) {
			f.SoftDelete = true
		}
	}
}

// ─────────────────────────────────────────────────────────────
//  Naming utilities
// ─────────────────────────────────────────────────────────────

// toSnakeCase converts "UserProfile" → "user_profile".
// Handles consecutive uppercase (e.g. "HTTPSProxy" → "https_proxy", "ID" → "id").
//
// Algorithm:
//  1. Insert underscore before an uppercase letter that follows a lowercase letter.
//  2. Insert underscore before an uppercase letter that is followed by a lowercase letter,
//     when the preceding character is also uppercase (e.g. "HTTPSProxy" → "https_proxy").
func toSnakeCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	n := len(runes)
	var buf strings.Builder
	buf.Grow(n + 4)

	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) {
					// e.g. "userProfile" → before 'P'
					buf.WriteByte('_')
				} else if i+1 < n && unicode.IsLower(runes[i+1]) && unicode.IsUpper(prev) {
					// e.g. "HTTPSProxy" → before 'P' (after "HTTPS")
					buf.WriteByte('_')
				}
			}
			buf.WriteRune(unicode.ToLower(r))
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// toSnakePlural converts "User" → "users", "UserProfile" → "user_profiles".
// Very basic English pluralisation — covers the 95% case.
func toSnakePlural(s string) string {
	snake := toSnakeCase(s)
	return pluralise(snake)
}

// pluralise applies naive English plural rules.
func pluralise(s string) string {
	if s == "" {
		return s
	}
	// Irregular
	irregulars := map[string]string{
		"person": "people", "man": "men", "woman": "women",
		"child": "children", "tooth": "teeth", "foot": "feet",
		"mouse": "mice", "goose": "geese", "ox": "oxen",
		"criterion": "criteria", "datum": "data", "medium": "media",
	}
	if p, ok := irregulars[s]; ok {
		return p
	}
	// Rules
	switch {
	case strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") ||
		strings.HasSuffix(s, "z") || strings.HasSuffix(s, "ch") ||
		strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y") && len(s) > 1 &&
		!isVowel(rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "f") && !strings.HasSuffix(s, "ff"):
		return s[:len(s)-1] + "ves"
	case strings.HasSuffix(s, "fe"):
		return s[:len(s)-2] + "ves"
	default:
		return s + "s"
	}
}

func isVowel(r rune) bool {
	return r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u'
}
