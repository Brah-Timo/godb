package builder

import "fmt"

// buildDelete constructs a DELETE FROM … WHERE … statement.
func buildDelete(c *Chain) (string, []interface{}) {
	ac := &argCounter{}
	whereSQL, whereArgs := buildWhere(c, ac)

	q := fmt.Sprintf("DELETE FROM %s", c.dialect.QuoteIdent(c.tableForModel()))
	if whereSQL != "" {
		q += " WHERE " + whereSQL
	}
	return q, whereArgs
}
