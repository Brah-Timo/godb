package builder

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Brah-Timo/godb/schema"
)

// buildUpdate constructs an UPDATE … SET … WHERE … statement.
// value can be a struct (non-zero fields) or map[string]interface{}.
func buildUpdate(c *Chain, value interface{}, m *schema.Model) (string, []interface{}, error) {
	var setCols []string
	var args []interface{}
	ac := &argCounter{}

	switch v := value.(type) {
	case map[string]interface{}:
		for col, val := range v {
			setCols = append(setCols, c.dialect.QuoteIdent(col)+" = "+c.dialect.ReplacePlaceholders("?", ac.n+1))
			ac.n++
			args = append(args, val)
		}

	default:
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			return "", nil, fmt.Errorf("godb.Updates: value must be a struct or map, got %T", value)
		}
		if m == nil {
			return "", nil, fmt.Errorf("godb.Updates: no model")
		}
		for _, f := range m.WritableFields(schema.OpUpdate) {
			fv := rv.FieldByIndex(f.StructIdx)
			if schema.IsZeroValue(fv) {
				continue // skip zero-value fields on struct update
			}
			setCols = append(setCols, c.dialect.QuoteIdent(f.Column)+" = "+c.dialect.ReplacePlaceholders("?", ac.n+1))
			ac.n++
			args = append(args, fv.Interface())
		}
	}

	if len(setCols) == 0 {
		return "", nil, fmt.Errorf("godb.Updates: no columns to update (all values are zero)")
	}

	table := c.tableForModel()
	whereSQL, whereArgs := buildWhere(c, ac)

	q := fmt.Sprintf("UPDATE %s SET %s",
		c.dialect.QuoteIdent(table),
		strings.Join(setCols, ", "),
	)
	if whereSQL != "" {
		q += " WHERE " + whereSQL
		args = append(args, whereArgs...)
	}

	return q, args, nil
}
