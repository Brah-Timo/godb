package builder

import (
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────────────────────
//  SELECT builder
// ─────────────────────────────────────────────────────────────

// buildSelect compiles the full SELECT SQL string and its ordered argument slice.
func buildSelect(c *Chain) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}
	ac := &argCounter{}

	// SELECT [DISTINCT]
	sb.WriteString("SELECT ")
	if c.distinct {
		sb.WriteString("DISTINCT ")
	}

	if len(c.selects) == 0 {
		sb.WriteString("*")
	} else {
		sb.WriteString(strings.Join(c.selects, ", "))
	}

	// FROM
	sb.WriteString(" FROM ")
	sb.WriteString(c.dialect.QuoteIdent(c.tableForModel()))

	// JOINs
	for _, j := range c.joins {
		fmt.Fprintf(&sb, " %s JOIN %s ON %s",
			j.kind,
			c.dialect.QuoteIdent(j.table),
			j.condition,
		)
		if len(j.args) > 0 {
			args = append(args, j.args...)
			ac.n += len(j.args)
		}
	}

	// WHERE
	whereSQL, whereArgs := buildWhere(c, ac)
	if whereSQL != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereSQL)
		args = append(args, whereArgs...)
	}

	// GROUP BY
	if len(c.groupBys) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(c.groupBys, ", "))
	}

	// HAVING
	if len(c.havings) > 0 {
		sb.WriteString(" HAVING ")
		for i, h := range c.havings {
			if i > 0 {
				sb.WriteString(" AND ")
			}
			repl := c.dialect.ReplacePlaceholders(h.query, ac.n+1)
			sb.WriteString(repl)
			ac.n += len(h.args)
			args = append(args, h.args...)
		}
	}

	// ORDER BY
	if len(c.orderBys) > 0 {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(strings.Join(c.orderBys, ", "))
	}

	// LIMIT / OFFSET
	lo := c.dialect.LimitOffset(c.limit, c.offset)
	if lo != "" {
		sb.WriteByte(' ')
		sb.WriteString(lo)
	}

	// FOR UPDATE
	if c.forUpdate {
		sb.WriteString(" FOR UPDATE")
	}

	return sb.String(), args
}

// buildCount compiles SELECT COUNT(*) …
func buildCount(c *Chain) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}
	ac := &argCounter{}

	sb.WriteString("SELECT COUNT(*) FROM ")
	sb.WriteString(c.dialect.QuoteIdent(c.tableForModel()))

	for _, j := range c.joins {
		fmt.Fprintf(&sb, " %s JOIN %s ON %s", j.kind, c.dialect.QuoteIdent(j.table), j.condition)
		args = append(args, j.args...)
		ac.n += len(j.args)
	}

	whereSQL, whereArgs := buildWhere(c, ac)
	if whereSQL != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereSQL)
		args = append(args, whereArgs...)
	}

	return sb.String(), args
}

// buildWhere assembles the WHERE clause (without the "WHERE" keyword).
// It handles AND/OR clauses and the soft-delete filter.
func buildWhere(c *Chain, ac *argCounter) (string, []interface{}) {
	var parts []string
	var args []interface{}

	// Automatic soft-delete filter
	if c.model != nil && c.model.HasSoftDelete && !c.unscoped {
		// Find the soft-delete column
		for _, f := range c.model.Fields {
			if f.SoftDelete {
				parts = append(parts, c.dialect.QuoteIdent(f.Column)+" IS NULL")
				break
			}
		}
	}

	// AND conditions
	for _, w := range c.wheres {
		repl := c.dialect.ReplacePlaceholders(w.query, ac.n+1)
		parts = append(parts, repl)
		ac.n += len(w.args)
		args = append(args, w.args...)
	}

	// OR conditions — grouped: prev AND (a OR b OR c)
	if len(c.orWheres) > 0 {
		orParts := make([]string, len(c.orWheres))
		for i, w := range c.orWheres {
			orParts[i] = c.dialect.ReplacePlaceholders(w.query, ac.n+1)
			ac.n += len(w.args)
			args = append(args, w.args...)
		}
		parts = append(parts, "("+strings.Join(orParts, " OR ")+")")
	}

	return strings.Join(parts, " AND "), args
}
