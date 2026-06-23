// Package utils provides shared internal utilities.
// Nothing in this package is exported to users of godb.
package utils

import (
	"strings"
	"unicode"
)

// ToSnakeCase converts CamelCase to snake_case.
// "UserName" → "user_name", "HTTPSProxy" → "https_proxy"
func ToSnakeCase(s string) string {
	runes := []rune(s)
	var buf strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextIsLower := i < len(runes)-1 && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || nextIsLower {
				buf.WriteByte('_')
			}
		}
		buf.WriteRune(unicode.ToLower(r))
	}
	return buf.String()
}

// ToCamelCase converts snake_case to CamelCase.
// "user_name" → "UserName"
func ToCamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// Plural applies a simple English pluralisation rule.
func Plural(s string) string {
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 {
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}
