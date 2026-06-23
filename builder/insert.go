package builder

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Brah-Timo/godb/schema"
)

// buildInsert constructs an INSERT INTO … VALUES (…) statement.
func buildInsert(c *Chain, rv reflect.Value, m *schema.Model) (string, []interface{}, error) {
	fields := m.WritableFields(schema.OpCreate)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("godb.Create: no writable fields on %s", m.Name)
	}

	cols := make([]string, 0, len(fields))
	placeholders := make([]string, 0, len(fields))
	args := make([]interface{}, 0, len(fields))
	ac := &argCounter{}

	for _, f := range fields {
		fv := rv.FieldByIndex(f.StructIdx)
		// Skip zero-value non-required fields to respect DB defaults
		if schema.IsZeroValue(fv) && !f.NotNull && f.Default == "" {
			continue
		}
		cols = append(cols, c.dialect.QuoteIdent(f.Column))
		placeholder := c.dialect.ReplacePlaceholders("?", ac.n+1)
		placeholders = append(placeholders, placeholder)
		ac.n++
		args = append(args, fv.Interface())
	}

	if len(cols) == 0 {
		return "", nil, fmt.Errorf("godb.Create: all fields are zero-value on %s", m.Name)
	}

	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		c.dialect.QuoteIdent(c.tableForModel()),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)
	return q, args, nil
}

// buildUpsert constructs INSERT … ON CONFLICT DO UPDATE SET …
func buildUpsert(c *Chain, rv reflect.Value, m *schema.Model, conflictCols []string) (string, []interface{}, error) {
	insertSQL, args, err := buildInsert(c, rv, m)
	if err != nil {
		return "", nil, err
	}

	// Determine update columns (all non-conflict, non-PK writable fields)
	updateCols := make([]string, 0)
	for _, f := range m.WritableFields(schema.OpUpdate) {
		isConflict := false
		for _, cc := range conflictCols {
			if cc == f.Column {
				isConflict = true
				break
			}
		}
		if !isConflict {
			updateCols = append(updateCols, f.Column)
		}
	}

	suffix := c.dialect.OnConflictDoUpdate(conflictCols, updateCols)
	return insertSQL + " " + suffix, args, nil
}

// buildBulkInsert constructs a single INSERT with multiple value rows.
// values must be a slice of structs.
func buildBulkInsert(c *Chain, values reflect.Value, m *schema.Model) (string, []interface{}, error) {
	if values.Len() == 0 {
		return "", nil, fmt.Errorf("godb.BulkCreate: empty slice")
	}

	fields := m.WritableFields(schema.OpCreate)
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = c.dialect.QuoteIdent(f.Column)
	}

	var rowPlaceholders []string
	var args []interface{}
	ac := &argCounter{}

	for i := 0; i < values.Len(); i++ {
		rv := values.Index(i)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		fillAutoTime(rv, m, c.now(), schema.OpCreate)

		phs := make([]string, len(fields))
		for j, f := range fields {
			fv := rv.FieldByIndex(f.StructIdx)
			phs[j] = c.dialect.ReplacePlaceholders("?", ac.n+1)
			ac.n++
			args = append(args, fv.Interface())
		}
		rowPlaceholders = append(rowPlaceholders, "("+strings.Join(phs, ", ")+")")
	}

	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		c.dialect.QuoteIdent(c.tableForModel()),
		strings.Join(cols, ", "),
		strings.Join(rowPlaceholders, ", "),
	)
	return q, args, nil
}
