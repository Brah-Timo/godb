package schema

// ToSnakeCase is the public version of toSnakeCase (used by relations package).
func ToSnakeCase(s string) string {
	return toSnakeCase(s)
}
