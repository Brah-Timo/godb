package builder

import (
	"fmt"
	"reflect"
	"time"

	"github.com/Brah-Timo/godb/schema"
)

// BulkCreate inserts a slice of records in a single statement.
// values must be a pointer to a slice of structs.
//
//	users := []User{{Name: "A"}, {Name: "B"}}
//	db.Model(&User{}).BulkCreate(&users)
func (c *Chain) BulkCreate(values interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}

	rv := reflect.ValueOf(values)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("godb.BulkCreate: values must be a slice, got %T", values)
	}

	m := c.model
	if m == nil {
		return fmt.Errorf("godb.BulkCreate: no model set")
	}

	q, args, err := buildBulkInsert(c, rv, m)
	if err != nil {
		return err
	}

	if c.dryRun {
		c.logger.LogInfof("DRY RUN BULK INSERT: %s %v", q, args)
		return nil
	}

	start := time.Now()
	_, execErr := c.dbExec(q, args)
	c.logAndRecord(q, args, time.Since(start), "BULK_INSERT", execErr)

	if execErr != nil {
		return wrapDBErr(execErr, q)
	}
	if c.cacheImpl != nil {
		_ = c.cacheImpl.InvalidateTable(c.tableForModel())
	}
	return nil
}

// BulkUpdate updates multiple records efficiently using a CASE WHEN expression.
// values must be a slice of structs where each element has its primary key set.
func (c *Chain) BulkUpdate(values interface{}, columns []string) error {
	if err := c.checkErr(); err != nil {
		return err
	}

	rv := reflect.ValueOf(values)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice || rv.Len() == 0 {
		return nil
	}

	m := c.model
	if m == nil || len(m.PrimaryKeys) == 0 {
		return fmt.Errorf("godb.BulkUpdate: model must have a primary key")
	}
	pkField := m.PrimaryKeys[0]
	pkCol := c.dialect.QuoteIdent(pkField.Column)

	// Collect IDs and build CASE WHEN for each column
	ids := make([]interface{}, rv.Len())
	cases := make(map[string][]string, len(columns))
	caseArgs := make(map[string][]interface{}, len(columns))

	ac := &argCounter{}

	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i)
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		fillAutoTime(elem, m, c.now(), schema.OpUpdate)

		pkVal := elem.FieldByIndex(pkField.StructIdx).Interface()
		ids[i] = pkVal

		for _, col := range columns {
			f, ok := m.ByColumn[col]
			if !ok {
				continue
			}
			fv := elem.FieldByIndex(f.StructIdx)
			ph1 := c.dialect.ReplacePlaceholders("?", ac.n+1)
			ph2 := c.dialect.ReplacePlaceholders("?", ac.n+2)
			ac.n += 2
			cases[col] = append(cases[col],
				fmt.Sprintf("WHEN %s = %s THEN %s", pkCol, ph1, ph2))
			caseArgs[col] = append(caseArgs[col], pkVal, fv.Interface())
		}
	}

	// Build the final SQL
	var setCols []string
	var finalArgs []interface{}
	ac2 := &argCounter{}
	for _, col := range columns {
		whens := cases[col]
		caseExpr := fmt.Sprintf("CASE %s END", joinStrings(whens, " "))
		setCols = append(setCols, c.dialect.QuoteIdent(col)+" = "+caseExpr)
		finalArgs = append(finalArgs, caseArgs[col]...)
		ac2.n += len(caseArgs[col])
	}

	// WHERE id IN (…)
	inPH := c.dialect.ReplacePlaceholders(makePlaceholders(len(ids)), ac2.n+1)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE %s IN (%s)",
		c.dialect.QuoteIdent(c.tableForModel()),
		joinStrings(setCols, ", "),
		pkCol,
		inPH,
	)
	finalArgs = append(finalArgs, ids...)

	start := time.Now()
	_, execErr := c.dbExec(q, finalArgs)
	c.logAndRecord(q, finalArgs, time.Since(start), "BULK_UPDATE", execErr)
	if execErr != nil {
		return wrapDBErr(execErr, q)
	}
	if c.cacheImpl != nil {
		_ = c.cacheImpl.InvalidateTable(c.tableForModel())
	}
	return nil
}

func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
