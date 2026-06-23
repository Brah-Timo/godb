package schema

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
)

// ─────────────────────────────────────────────────────────────
//  ScanRows — maps sql.Rows into Go values
// ─────────────────────────────────────────────────────────────

// ScanRows reads all rows from rows and appends the decoded values into
// destSlice (which must be a pointer to a slice: *[]T or *[]*T).
//
// If model is non-nil, it uses the Model's column map for efficient field
// lookup and type conversion.
// If model is nil (e.g. for ad-hoc Scan), it falls back to positional scanning.
func ScanRows(rows *sql.Rows, model *Model, destSlice interface{}) error {
	dv := reflect.ValueOf(destSlice)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("schema.ScanRows: dest must be a non-nil pointer to a slice")
	}
	sliceVal := dv.Elem()
	sliceType := sliceVal.Type()
	if sliceType.Kind() != reflect.Slice {
		return fmt.Errorf("schema.ScanRows: dest must point to a slice, got %s", sliceType.Kind())
	}

	elemType := sliceType.Elem()
	isPtr := elemType.Kind() == reflect.Ptr
	if isPtr {
		elemType = elemType.Elem()
	}

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("schema.ScanRows: columns: %w", err)
	}

	for rows.Next() {
		elem := reflect.New(elemType) // always *T

		if err := scanRow(rows, cols, model, elem); err != nil {
			return err
		}

		if isPtr {
			sliceVal = reflect.Append(sliceVal, elem)
		} else {
			sliceVal = reflect.Append(sliceVal, elem.Elem())
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("schema.ScanRows: rows iteration: %w", err)
	}

	dv.Elem().Set(sliceVal)
	return nil
}

// ScanRow reads exactly one row from rows into dest (pointer to struct or map).
func ScanRow(rows *sql.Rows, model *Model, dest interface{}) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("schema.ScanRow: dest must be a pointer")
	}
	return scanRow(rows, cols, model, rv)
}

// ScanSingleRow reads from a *sql.Row into dest.
func ScanSingleRow(row *sql.Row, model *Model, dest interface{}) error {
	if model == nil {
		return row.Scan(dest)
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return row.Scan(dest)
	}

	// We can't get column names from *sql.Row without executing, so we
	// build scanners in primary-key / all-fields order. For this path
	// the caller must ensure column order matches field order.
	fields := model.ColumnFields()
	scanners := make([]interface{}, len(fields))
	elem := rv.Elem()
	for i, f := range fields {
		fv := elem.FieldByIndex(f.StructIdx)
		scanners[i] = fv.Addr().Interface()
	}
	return row.Scan(scanners...)
}

// scanRow fills a single *T reflect.Value from the current rows position.
func scanRow(rows *sql.Rows, cols []string, model *Model, dest reflect.Value) error {
	elem := dest.Elem()

	if elem.Kind() == reflect.Map {
		return scanRowIntoMap(rows, cols, elem)
	}
	if elem.Kind() != reflect.Struct {
		// Primitive destination (e.g. for Count())
		scanners := []interface{}{dest.Interface()}
		return rows.Scan(scanners...)
	}

	scanners := make([]interface{}, len(cols))

	for i, col := range cols {
		if model != nil {
			if f, ok := model.ByColumn[col]; ok {
				fv := elem.FieldByIndex(f.StructIdx)
				if fv.IsValid() && fv.CanAddr() {
					scanners[i] = fv.Addr().Interface()
					continue
				}
			}
		}
		// Fallback: try to match by snake_case of exported field names
		if fv := elem.FieldByName(snakeToCamel(col)); fv.IsValid() && fv.CanAddr() {
			scanners[i] = fv.Addr().Interface()
			continue
		}
		// Discard the column
		var discard interface{}
		scanners[i] = &discard
	}

	return rows.Scan(scanners...)
}

// scanRowIntoMap fills a map[string]interface{} from the current row.
func scanRowIntoMap(rows *sql.Rows, cols []string, mapVal reflect.Value) error {
	if mapVal.IsNil() {
		mapVal.Set(reflect.MakeMap(mapVal.Type()))
	}
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return err
	}
	for i, col := range cols {
		mapVal.SetMapIndex(reflect.ValueOf(col), reflect.ValueOf(vals[i]))
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
//  JSON scanning helper
// ─────────────────────────────────────────────────────────────

// JSONScanner implements sql.Scanner for fields that store JSON blobs.
type JSONScanner struct{ V interface{} }

func (j *JSONScanner) Scan(src interface{}) error {
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	case nil:
		return nil
	default:
		return fmt.Errorf("JSONScanner: unsupported source type %T", src)
	}
	return json.Unmarshal(b, j.V)
}

// ─────────────────────────────────────────────────────────────
//  Naming helpers
// ─────────────────────────────────────────────────────────────

// snakeToCamel converts "user_name" → "UserName" for fallback field lookup.
func snakeToCamel(s string) string {
	parts := splitWords(s)
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = string([]rune{[]rune(p)[0] - 32}) + p[1:]
	}
	joined := ""
	for _, p := range parts {
		joined += p
	}
	return joined
}

func splitWords(s string) []string {
	var words []string
	for _, w := range splitByUnderscore(s) {
		if w != "" {
			words = append(words, w)
		}
	}
	return words
}

func splitByUnderscore(s string) []string {
	out := []string{""}
	for _, c := range s {
		if c == '_' {
			out = append(out, "")
		} else {
			out[len(out)-1] += string(c)
		}
	}
	return out
}
