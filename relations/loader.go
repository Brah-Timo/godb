// Package relations implements eager loading (Preload) for godb.
//
// The key design principle is batch loading: instead of issuing N queries for N
// parent records (the N+1 problem), godb issues exactly ONE query per relation,
// fetching all related records in a single IN clause and then distributing them
// in Go.
//
// Example: Preload("Posts") on 100 users issues:
//
//	SELECT * FROM posts WHERE user_id IN (1,2,…,100)
//
// instead of 100 separate queries.
package relations

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

// Loader handles eager loading of relations for a result set.
type Loader struct {
	db       *sql.DB
	dialect  dialects.Dialect
	registry *schema.Registry
	ctx      context.Context
}

// NewLoader creates a Loader.
func NewLoader(db *sql.DB, d dialects.Dialect, r *schema.Registry, ctx context.Context) *Loader {
	return &Loader{db: db, dialect: d, registry: r, ctx: ctx}
}

// Load eagerly loads the named relations into the result slice.
// name may be dot-separated for nested relations: "Posts.Comments".
func (l *Loader) Load(results interface{}, parentModel *schema.Model, relations []string) error {
	for _, rel := range relations {
		if err := l.loadOne(results, parentModel, rel); err != nil {
			return fmt.Errorf("preload %q: %w", rel, err)
		}
	}
	return nil
}

// loadOne loads a single (possibly nested) relation.
func (l *Loader) loadOne(results interface{}, parentModel *schema.Model, relation string) error {
	parts := strings.SplitN(relation, ".", 2)
	relName := parts[0]
	nested := ""
	if len(parts) > 1 {
		nested = parts[1]
	}

	// Find the relation field on the parent model
	relField := findRelationField(parentModel, relName)
	if relField == nil {
		return fmt.Errorf("relations: field %q not found on %s", relName, parentModel.Name)
	}
	if relField.Relation == nil {
		return fmt.Errorf("relations: field %q on %s is not a relation", relName, parentModel.Name)
	}

	rv := reflect.ValueOf(results)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	switch relField.Relation.Type {
	case schema.RelHasMany:
		if err := l.loadHasMany(rv, parentModel, relField); err != nil {
			return err
		}
	case schema.RelHasOne:
		if err := l.loadHasOne(rv, parentModel, relField); err != nil {
			return err
		}
	case schema.RelBelongsTo:
		if err := l.loadBelongsTo(rv, parentModel, relField); err != nil {
			return err
		}
	case schema.RelManyToMany:
		if err := l.loadManyToMany(rv, parentModel, relField); err != nil {
			return err
		}
	}

	// Load nested relations if requested
	if nested != "" {
		childModel, err := l.childModel(relField)
		if err != nil {
			return err
		}
		// Collect all loaded children
		children := collectChildren(rv, relField)
		return l.Load(children.Interface(), childModel, []string{nested})
	}

	return nil
}

// ─────────────────────────────────────────────────────────────
//  HasMany  (parent has many children, FK on child)
// ─────────────────────────────────────────────────────────────

func (l *Loader) loadHasMany(parents reflect.Value, parentModel *schema.Model, relField *schema.Field) error {
	rel := relField.Relation
	fk := rel.ForeignKey
	if fk == "" {
		fk = schema.ToSnakeCase(parentModel.Name) + "_id"
	}

	ids := collectParentIDs(parents, parentModel)
	if len(ids) == 0 {
		return nil
	}

	childModel, err := l.childModel(relField)
	if err != nil {
		return err
	}

	rows, err := l.batchQuery(childModel.Table, fk, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Build parent ID → children map
	childType := relField.GoType
	if childType.Kind() == reflect.Slice {
		childType = childType.Elem()
	}
	if childType.Kind() == reflect.Ptr {
		childType = childType.Elem()
	}

	pkField := parentModel.PrimaryKeys[0]
	cols, _ := rows.Columns()

	childMap := make(map[interface{}]reflect.Value) // parentID → []Child

	for rows.Next() {
		child := reflect.New(childType).Elem()
		scanners := buildScanners(cols, childModel, child)
		if err := rows.Scan(scanners...); err != nil {
			return err
		}
		// Get the FK value from child
		fkF := childModel.ByColumn[fk]
		if fkF == nil {
			continue
		}
		fkVal := child.FieldByIndex(fkF.StructIdx).Interface()
		childMap[fkVal] = reflect.Append(childMap[fkVal], child)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Assign children to parents
	for i := 0; i < parents.Len(); i++ {
		parent := parents.Index(i)
		if parent.Kind() == reflect.Ptr {
			parent = parent.Elem()
		}
		pkVal := parent.FieldByIndex(pkField.StructIdx).Interface()
		relFV := parent.FieldByIndex(relField.StructIdx)
		if children, ok := childMap[pkVal]; ok {
			relFV.Set(children)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
//  HasOne  (parent has one child, FK on child)
// ─────────────────────────────────────────────────────────────

func (l *Loader) loadHasOne(parents reflect.Value, parentModel *schema.Model, relField *schema.Field) error {
	rel := relField.Relation
	fk := rel.ForeignKey
	if fk == "" {
		fk = schema.ToSnakeCase(parentModel.Name) + "_id"
	}

	ids := collectParentIDs(parents, parentModel)
	if len(ids) == 0 {
		return nil
	}

	childModel, err := l.childModel(relField)
	if err != nil {
		return err
	}

	rows, err := l.batchQuery(childModel.Table, fk, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	childType := relField.GoType
	if childType.Kind() == reflect.Ptr {
		childType = childType.Elem()
	}

	pkField := parentModel.PrimaryKeys[0]
	cols, _ := rows.Columns()

	childMap := make(map[interface{}]reflect.Value)
	for rows.Next() {
		child := reflect.New(childType).Elem()
		scanners := buildScanners(cols, childModel, child)
		if err := rows.Scan(scanners...); err != nil {
			return err
		}
		fkF := childModel.ByColumn[fk]
		if fkF == nil {
			continue
		}
		fkVal := child.FieldByIndex(fkF.StructIdx).Interface()
		childMap[fkVal] = child
	}

	for i := 0; i < parents.Len(); i++ {
		parent := parents.Index(i)
		if parent.Kind() == reflect.Ptr {
			parent = parent.Elem()
		}
		pkVal := parent.FieldByIndex(pkField.StructIdx).Interface()
		if child, ok := childMap[pkVal]; ok {
			relFV := parent.FieldByIndex(relField.StructIdx)
			if relFV.Kind() == reflect.Ptr {
				ptr := reflect.New(child.Type())
				ptr.Elem().Set(child)
				relFV.Set(ptr)
			} else {
				relFV.Set(child)
			}
		}
	}
	return rows.Err()
}

// ─────────────────────────────────────────────────────────────
//  BelongsTo  (child holds FK to parent)
// ─────────────────────────────────────────────────────────────

func (l *Loader) loadBelongsTo(children reflect.Value, childModel *schema.Model, relField *schema.Field) error {
	rel := relField.Relation
	parentModel, err := l.childModel(relField) // confusingly, childModel here is the "belongs-to" target
	if err != nil {
		return err
	}

	fk := rel.ForeignKey
	if fk == "" {
		fk = schema.ToSnakeCase(parentModel.Name) + "_id"
	}

	// Collect FK values from children
	fkField := childModel.ByColumn[fk]
	if fkField == nil {
		return fmt.Errorf("belongs_to: FK column %q not found in %s", fk, childModel.Name)
	}

	ids := make([]interface{}, 0, children.Len())
	for i := 0; i < children.Len(); i++ {
		child := children.Index(i)
		if child.Kind() == reflect.Ptr {
			child = child.Elem()
		}
		ids = append(ids, child.FieldByIndex(fkField.StructIdx).Interface())
	}

	parentPKCol := "id"
	if len(parentModel.PrimaryKeys) > 0 {
		parentPKCol = parentModel.PrimaryKeys[0].Column
	}

	rows, err := l.batchQuery(parentModel.Table, parentPKCol, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	parentType := relField.GoType
	if parentType.Kind() == reflect.Ptr {
		parentType = parentType.Elem()
	}

	cols, _ := rows.Columns()
	parentMap := make(map[interface{}]reflect.Value)
	for rows.Next() {
		parent := reflect.New(parentType).Elem()
		scanners := buildScanners(cols, parentModel, parent)
		if err := rows.Scan(scanners...); err != nil {
			return err
		}
		if len(parentModel.PrimaryKeys) == 0 {
			continue
		}
		pkVal := parent.FieldByIndex(parentModel.PrimaryKeys[0].StructIdx).Interface()
		parentMap[pkVal] = parent
	}

	for i := 0; i < children.Len(); i++ {
		child := children.Index(i)
		if child.Kind() == reflect.Ptr {
			child = child.Elem()
		}
		fkVal := child.FieldByIndex(fkField.StructIdx).Interface()
		if parent, ok := parentMap[fkVal]; ok {
			relFV := child.FieldByIndex(relField.StructIdx)
			if relFV.Kind() == reflect.Ptr {
				ptr := reflect.New(parent.Type())
				ptr.Elem().Set(parent)
				relFV.Set(ptr)
			} else {
				relFV.Set(parent)
			}
		}
	}
	return rows.Err()
}

// ─────────────────────────────────────────────────────────────
//  ManyToMany  (via join table)
// ─────────────────────────────────────────────────────────────

func (l *Loader) loadManyToMany(parents reflect.Value, parentModel *schema.Model, relField *schema.Field) error {
	rel := relField.Relation
	joinTable := rel.JoinTable
	if joinTable == "" {
		return fmt.Errorf("many_to_many: join_table not specified for field %s", relField.Name)
	}

	parentFK := rel.JoinFK
	if parentFK == "" {
		parentFK = schema.ToSnakeCase(parentModel.Name) + "_id"
	}

	ids := collectParentIDs(parents, parentModel)
	if len(ids) == 0 {
		return nil
	}

	childModel, err := l.childModel(relField)
	if err != nil {
		return err
	}

	refFK := rel.JoinRefFK
	if refFK == "" {
		refFK = schema.ToSnakeCase(childModel.Name) + "_id"
	}

	childPK := "id"
	if len(childModel.PrimaryKeys) > 0 {
		childPK = childModel.PrimaryKeys[0].Column
	}

	placeholders := makePlaceholders(len(ids))
	q := fmt.Sprintf(
		`SELECT j.%s, c.* FROM %s j
		 INNER JOIN %s c ON c.%s = j.%s
		 WHERE j.%s IN (%s)`,
		l.dialect.QuoteIdent(parentFK),
		l.dialect.QuoteIdent(joinTable),
		l.dialect.QuoteIdent(childModel.Table),
		l.dialect.QuoteIdent(childPK),
		l.dialect.QuoteIdent(refFK),
		l.dialect.QuoteIdent(parentFK),
		placeholders,
	)

	rows, err := l.db.QueryContext(l.ctx, q, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	childType := relField.GoType
	if childType.Kind() == reflect.Slice {
		childType = childType.Elem()
	}
	if childType.Kind() == reflect.Ptr {
		childType = childType.Elem()
	}

	cols, _ := rows.Columns()
	pkField := parentModel.PrimaryKeys[0]
	childMap := make(map[interface{}]reflect.Value)

	for rows.Next() {
		var parentID interface{}
		child := reflect.New(childType).Elem()
		scanners := append([]interface{}{&parentID}, buildScanners(cols[1:], childModel, child)...)
		if err := rows.Scan(scanners...); err != nil {
			return err
		}
		childMap[parentID] = reflect.Append(childMap[parentID], child)
	}

	for i := 0; i < parents.Len(); i++ {
		parent := parents.Index(i)
		if parent.Kind() == reflect.Ptr {
			parent = parent.Elem()
		}
		pkVal := parent.FieldByIndex(pkField.StructIdx).Interface()
		if children, ok := childMap[pkVal]; ok {
			parent.FieldByIndex(relField.StructIdx).Set(children)
		}
	}
	return rows.Err()
}

// ─────────────────────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────────────────────

func (l *Loader) batchQuery(table, col string, ids []interface{}) (*sql.Rows, error) {
	placeholders := makePlaceholders(len(ids))
	q := fmt.Sprintf("SELECT * FROM %s WHERE %s IN (%s)",
		l.dialect.QuoteIdent(table),
		l.dialect.QuoteIdent(col),
		placeholders,
	)
	return l.db.QueryContext(l.ctx, q, ids...)
}

func (l *Loader) childModel(relField *schema.Field) (*schema.Model, error) {
	t := relField.GoType
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return l.registry.ParseType(t)
}

func collectParentIDs(parents reflect.Value, parentModel *schema.Model) []interface{} {
	if len(parentModel.PrimaryKeys) == 0 {
		return nil
	}
	pkField := parentModel.PrimaryKeys[0]
	ids := make([]interface{}, 0, parents.Len())
	for i := 0; i < parents.Len(); i++ {
		parent := parents.Index(i)
		if parent.Kind() == reflect.Ptr {
			parent = parent.Elem()
		}
		ids = append(ids, parent.FieldByIndex(pkField.StructIdx).Interface())
	}
	return ids
}

func findRelationField(m *schema.Model, name string) *schema.Field {
	for _, f := range m.Fields {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

func collectChildren(parents reflect.Value, relField *schema.Field) reflect.Value {
	childType := relField.GoType
	if childType.Kind() == reflect.Slice {
		childType = childType.Elem()
	}
	var out reflect.Value
	for i := 0; i < parents.Len(); i++ {
		parent := parents.Index(i)
		if parent.Kind() == reflect.Ptr {
			parent = parent.Elem()
		}
		sliceVal := parent.FieldByIndex(relField.StructIdx)
		for j := 0; j < sliceVal.Len(); j++ {
			out = reflect.Append(out, sliceVal.Index(j))
		}
	}
	return out
}

func buildScanners(cols []string, m *schema.Model, structVal reflect.Value) []interface{} {
	scanners := make([]interface{}, len(cols))
	for i, col := range cols {
		var fv reflect.Value
		if m != nil {
			if f, ok := m.ByColumn[col]; ok {
				fv = structVal.FieldByIndex(f.StructIdx)
			}
		}
		if fv.IsValid() && fv.CanAddr() {
			scanners[i] = fv.Addr().Interface()
		} else {
			var discard interface{}
			scanners[i] = &discard
		}
	}
	return scanners
}

func makePlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}
