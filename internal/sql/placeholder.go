// Package sql provides internal SQL-generation utilities.
package sql

import (
	"fmt"
	"strings"
)

// ReplacePlaceholders replaces all ? in query with $n-style placeholders,
// starting at startAt. Returns the modified query and the next placeholder index.
func ReplacePlaceholders(query string, startAt int) (string, int) {
	var sb strings.Builder
	idx := startAt
	for _, ch := range query {
		if ch == '?' {
			fmt.Fprintf(&sb, "$%d", idx)
			idx++
		} else {
			sb.WriteRune(ch)
		}
	}
	return sb.String(), idx
}

// Placeholders returns a comma-joined list of n '?' placeholders.
func Placeholders(n int) string {
	if n == 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

// NumberedPlaceholders returns "$1, $2, … $n" (PostgreSQL style).
func NumberedPlaceholders(n, startAt int) string {
	if n == 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("$%d", startAt+i)
	}
	return strings.Join(parts, ", ")
}
