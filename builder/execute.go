package builder

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Brah-Timo/godb/cache"
	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  Read operations
// ─────────────────────────────────────────────────────────────

// Find executes the SELECT and scans all result rows into dest.
// dest must be a pointer to a slice: *[]User or *[]*User.
//
//	var users []User
//	err := db.Model(&User{}).Where("age > ?", 18).Find(&users)
func (c *Chain) Find(dest interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	if c.rawSQL != "" {
		return c.Scan(dest)
	}

	q, args := buildSelect(c)
	return c.execSelect(q, args, "SELECT", dest)
}

// First returns the first row matching the WHERE conditions.
// If no row is found, it returns ErrRecordNotFound.
func (c *Chain) First(dest interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	nc := c.Limit(1)
	// Default order: primary key ASC
	if len(nc.orderBys) == 0 && nc.model != nil && len(nc.model.PrimaryKeys) > 0 {
		nc = nc.Order(nc.dialect.QuoteIdent(nc.model.PrimaryKeys[0].Column) + " ASC")
	}

	q, args := buildSelect(nc)
	rows, err := c.dbQuery(q, args)
	if err != nil {
		return wrapDBErr(err, q)
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	if !rows.Next() {
		if e := rows.Err(); e != nil {
			return e
		}
		return errNotFound()
	}

	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("godb.First: dest must be a pointer, got %T", dest)
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("godb.First: dest must be a pointer to struct")
	}

	scanners := buildScanners(cols, c.model, elem)
	if err := rows.Scan(scanners...); err != nil {
		return err
	}

	c.logAndRecord(q, args, 0, "SELECT", nil)
	return nil
}

// Last returns the last row (ordered by primary key DESC).
func (c *Chain) Last(dest interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	nc := c.Limit(1)
	if len(nc.orderBys) == 0 && nc.model != nil && len(nc.model.PrimaryKeys) > 0 {
		nc = nc.Order(nc.dialect.QuoteIdent(nc.model.PrimaryKeys[0].Column) + " DESC")
	}
	return nc.First(dest)
}

// Take returns one row without enforcing any ordering.
func (c *Chain) Take(dest interface{}) error {
	return c.Limit(1).First(dest)
}

// Count returns the number of rows matching the WHERE conditions.
func (c *Chain) Count() (int64, error) {
	if err := c.checkErr(); err != nil {
		return 0, err
	}
	q, args := buildCount(c)
	start := time.Now()
	row := c.dbQueryRow(q, args)
	var n int64
	err := row.Scan(&n)
	c.logAndRecord(q, args, time.Since(start), "COUNT", err)
	if err != nil {
		return 0, wrapDBErr(err, q)
	}
	return n, nil
}

// Exists returns true if at least one row matches the WHERE conditions.
func (c *Chain) Exists() (bool, error) {
	n, err := c.Limit(1).Count()
	return n > 0, err
}

// Pluck fills a slice with a single column from the result set.
//
//	var names []string
//	db.Model(&User{}).Pluck("name", &names)
func (c *Chain) Pluck(column string, dest interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	return c.Select(c.dialect.QuoteIdent(column)).Find(dest)
}

// Scan executes a Raw query and scans the result into dest.
// dest can be *[]struct, *struct, or *[]map[string]interface{}.
func (c *Chain) Scan(dest interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	q := c.rawSQL
	args := c.rawArgs
	if q == "" {
		q, args = buildSelect(c)
	}
	return c.execSelect(q, args, "SCAN", dest)
}

// ─────────────────────────────────────────────────────────────
//  Write operations
// ─────────────────────────────────────────────────────────────

// Create inserts value into the database and populates auto-generated fields (ID, timestamps).
// value must be a pointer to a struct.
func (c *Chain) Create(value interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("godb.Create: value must be a pointer to a struct")
	}

	m := c.model
	if m == nil {
		return fmt.Errorf("godb.Create: no model set — use db.Model(&T{}).Create(&t)")
	}

	// Validate
	if m.HasValidate {
		if v, ok := value.(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return err
			}
		}
	}

	// BeforeCreate hook
	if m.HasBeforeCreate {
		if h, ok := value.(interface{ BeforeCreate() error }); ok {
			if err := h.BeforeCreate(); err != nil {
				return err
			}
		}
	}

	// Fill auto-time fields
	now := c.now()
	fillAutoTime(rv.Elem(), m, now, schema.OpCreate)

	// Build INSERT
	q, args, err := buildInsert(c, rv.Elem(), m)
	if err != nil {
		return err
	}

	if c.dryRun {
		c.logger.LogInfof("DRY RUN INSERT: %s %v", q, args)
		return nil
	}

	start := time.Now()
	res, execErr := c.dbExec(q, args)
	elapsed := time.Since(start)
	c.logAndRecord(q, args, elapsed, "INSERT", execErr)

	if execErr != nil {
		return wrapDBErr(execErr, q)
	}

	// Back-fill auto-increment primary key
	if id, e := res.LastInsertId(); e == nil && id > 0 {
		setPrimaryKey(rv.Elem(), m, id)
	}

	// Invalidate cache
	if c.cacheImpl != nil {
		_ = c.cacheImpl.InvalidateTable(c.tableForModel())
	}

	// AfterCreate hook
	if m.HasAfterCreate {
		if h, ok := value.(interface{ AfterCreate() }); ok {
			h.AfterCreate()
		}
	}

	return nil
}

// Updates updates all matching rows with the values in value.
// value can be a struct (only non-zero fields are set) or map[string]interface{}.
//
//	db.Model(&User{}).Where("id = ?", 1).Updates(User{Name: "Alice"})
//	db.Model(&User{}).Where("id = ?", 1).Updates(map[string]interface{}{"name": "Alice"})
func (c *Chain) Updates(value interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	if len(c.wheres) == 0 && !c.unscoped {
		return errNoCondition()
	}

	m := c.model

	// BeforeUpdate hook
	if m != nil && m.HasBeforeUpdate {
		if h, ok := value.(interface{ BeforeUpdate() error }); ok {
			if err := h.BeforeUpdate(); err != nil {
				return err
			}
		}
	}

	// Fill UpdatedAt
	if m != nil && m.HasAutoUpdateTime {
		now := c.now()
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.Struct {
			fillAutoTime(rv, m, now, schema.OpUpdate)
		}
	}

	q, args, err := buildUpdate(c, value, m)
	if err != nil {
		return err
	}

	if c.dryRun {
		c.logger.LogInfof("DRY RUN UPDATE: %s %v", q, args)
		return nil
	}

	start := time.Now()
	_, execErr := c.dbExec(q, args)
	c.logAndRecord(q, args, time.Since(start), "UPDATE", execErr)

	if execErr != nil {
		return wrapDBErr(execErr, q)
	}
	if c.cacheImpl != nil {
		_ = c.cacheImpl.InvalidateTable(c.tableForModel())
	}
	if m != nil && m.HasAfterUpdate {
		if h, ok := value.(interface{ AfterUpdate() }); ok {
			h.AfterUpdate()
		}
	}
	return nil
}

// Update updates a single column.
//
//	db.Model(&User{}).Where("id = ?", 1).Update("name", "Alice")
func (c *Chain) Update(column string, value interface{}) error {
	return c.Updates(map[string]interface{}{column: value})
}

// Save inserts a new record or updates an existing one based on whether
// the primary key is set (upsert by primary key).
func (c *Chain) Save(value interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	m := c.model
	if m == nil || len(m.PrimaryKeys) == 0 {
		return c.Create(value)
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	pk := rv.FieldByIndex(m.PrimaryKeys[0].StructIdx)
	if schema.IsZeroValue(pk) {
		return c.Create(value)
	}

	return c.Where(
		c.dialect.QuoteIdent(m.PrimaryKeys[0].Column)+" = ?",
		pk.Interface(),
	).Updates(value)
}

// Delete deletes rows matching the WHERE conditions.
// If the model supports soft-delete, deleted_at is set instead of hard-deleting.
func (c *Chain) Delete(value ...interface{}) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	if len(c.wheres) == 0 && !c.unscoped {
		return errNoCondition()
	}

	m := c.model

	// BeforeDelete hook
	if m != nil && m.HasBeforeDelete && len(value) > 0 {
		if h, ok := value[0].(interface{ BeforeDelete() error }); ok {
			if err := h.BeforeDelete(); err != nil {
				return err
			}
		}
	}

	// Soft-delete path
	if !c.unscoped && m != nil && m.HasSoftDelete {
		return c.Updates(map[string]interface{}{"deleted_at": c.now()})
	}

	q, args := buildDelete(c)

	if c.dryRun {
		c.logger.LogInfof("DRY RUN DELETE: %s %v", q, args)
		return nil
	}

	start := time.Now()
	_, execErr := c.dbExec(q, args)
	c.logAndRecord(q, args, time.Since(start), "DELETE", execErr)

	if execErr != nil {
		return wrapDBErr(execErr, q)
	}
	if c.cacheImpl != nil {
		_ = c.cacheImpl.InvalidateTable(c.tableForModel())
	}
	if m != nil && m.HasAfterDelete && len(value) > 0 {
		if h, ok := value[0].(interface{ AfterDelete() }); ok {
			h.AfterDelete()
		}
	}
	return nil
}

// Upsert performs an INSERT … ON CONFLICT DO UPDATE SET … (all dialects).
func (c *Chain) Upsert(value interface{}, conflictCols []string) error {
	if err := c.checkErr(); err != nil {
		return err
	}
	m := c.model
	if m == nil {
		return fmt.Errorf("godb.Upsert: no model set")
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	fillAutoTime(rv, m, c.now(), schema.OpCreate)

	q, args, err := buildUpsert(c, rv, m, conflictCols)
	if err != nil {
		return err
	}
	start := time.Now()
	_, execErr := c.dbExec(q, args)
	c.logAndRecord(q, args, time.Since(start), "UPSERT", execErr)
	if execErr != nil {
		return wrapDBErr(execErr, q)
	}
	if c.cacheImpl != nil {
		_ = c.cacheImpl.InvalidateTable(c.tableForModel())
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
//  Internal DB call helpers — use the sqlDB interface directly
// ─────────────────────────────────────────────────────────────

// dbExec executes a write query using the chain's db (which is a sqlExecutor).
func (c *Chain) dbExec(q string, args []interface{}) (sql.Result, error) {
	return c.db.ExecContext(c.ctx, q, args...)
}

// dbQuery executes a SELECT and returns sql.Rows.
func (c *Chain) dbQuery(q string, args []interface{}) (*sql.Rows, error) {
	return c.db.QueryContext(c.ctx, q, args...)
}

// dbQueryRow executes a query returning a single row.
func (c *Chain) dbQueryRow(q string, args []interface{}) *sql.Row {
	return c.db.QueryRowContext(c.ctx, q, args...)
}

// ─────────────────────────────────────────────────────────────
//  SELECT execution + cache
// ─────────────────────────────────────────────────────────────

func (c *Chain) execSelect(q string, args []interface{}, op string, dest interface{}) error {
	// Cache read
	if c.cacheImpl != nil && c.cacheTTL > 0 {
		key := cache.BuildKey(c.tableForModel(), q, args)
		if data, err := c.cacheImpl.Get(key); err == nil {
			if err2 := json.Unmarshal(data, dest); err2 == nil {
				return nil
			}
		}
	}

	if c.dryRun {
		c.logger.LogInfof("DRY RUN %s: %s %v", op, q, args)
		return nil
	}

	start := time.Now()
	rows, err := c.dbQuery(q, args)
	elapsed := time.Since(start)
	c.logAndRecord(q, args, elapsed, op, err)
	if err != nil {
		return wrapDBErr(err, q)
	}
	defer rows.Close()

	if err := scanDest(rows, c.model, dest); err != nil {
		return err
	}

	// Cache write
	if c.cacheImpl != nil && c.cacheTTL > 0 {
		if data, merr := json.Marshal(dest); merr == nil {
			key := cache.BuildKey(c.tableForModel(), q, args)
			_ = c.cacheImpl.Set(key, data, c.cacheTTL)
		}
	}

	return nil
}

// ─────────────────────────────────────────────────────────────
//  Scan helpers
// ─────────────────────────────────────────────────────────────

// scanDest routes scanning based on the concrete type of dest.
func scanDest(rows *sql.Rows, m *schema.Model, dest interface{}) error {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("godb: dest must be a non-nil pointer")
	}
	elem := rv.Elem()

	switch elem.Kind() {
	case reflect.Slice:
		return scanSlice(rows, m, dest)
	case reflect.Struct:
		return scanStruct(rows, m, dest)
	case reflect.Map:
		return scanMap(rows, dest)
	default:
		// Primitive: int64, string, etc.
		if !rows.Next() {
			return errNotFound()
		}
		return rows.Scan(dest)
	}
}

func scanSlice(rows *sql.Rows, m *schema.Model, dest interface{}) error {
	rv := reflect.ValueOf(dest).Elem()
	sliceType := rv.Type()
	elemType := sliceType.Elem()
	isPtr := elemType.Kind() == reflect.Ptr
	if isPtr {
		elemType = elemType.Elem()
	}

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	for rows.Next() {
		elem := reflect.New(elemType)
		scanners := buildScanners(cols, m, elem.Elem())
		if err := rows.Scan(scanners...); err != nil {
			return err
		}
		if isPtr {
			rv = reflect.Append(rv, elem)
		} else {
			rv = reflect.Append(rv, elem.Elem())
		}
	}
	reflect.ValueOf(dest).Elem().Set(rv)
	return rows.Err()
}

func scanStruct(rows *sql.Rows, m *schema.Model, dest interface{}) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if !rows.Next() {
		if e := rows.Err(); e != nil {
			return e
		}
		return errNotFound()
	}
	rv := reflect.ValueOf(dest).Elem()
	scanners := buildScanners(cols, m, rv)
	return rows.Scan(scanners...)
}

func scanMap(rows *sql.Rows, dest interface{}) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(dest).Elem()
	if rv.IsNil() {
		rv.Set(reflect.MakeMap(rv.Type()))
	}
	if !rows.Next() {
		return rows.Err()
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
		rv.SetMapIndex(reflect.ValueOf(col), reflect.ValueOf(vals[i]))
	}
	return nil
}

// buildScanners creates a slice of scan targets aligned to cols.
func buildScanners(cols []string, m *schema.Model, structVal reflect.Value) []interface{} {
	scanners := make([]interface{}, len(cols))
	for i, col := range cols {
		var fv reflect.Value
		if m != nil {
			if f, ok := m.ByColumn[col]; ok {
				fv = structVal.FieldByIndex(f.StructIdx)
			}
		}
		if !fv.IsValid() {
			// Fallback: CamelCase column name match
			fv = structVal.FieldByName(snakeToCamelLocal(col))
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

// snakeToCamelLocal converts "user_name" → "UserName".
func snakeToCamelLocal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// ─────────────────────────────────────────────────────────────
//  Auto-time fill
// ─────────────────────────────────────────────────────────────

func fillAutoTime(rv reflect.Value, m *schema.Model, now time.Time, op schema.Operation) {
	for _, f := range m.Fields {
		if f.AutoCreateTime && op == schema.OpCreate {
			fv := rv.FieldByIndex(f.StructIdx)
			if fv.CanSet() {
				fv.Set(reflect.ValueOf(now).Convert(fv.Type()))
			}
		}
		if f.AutoUpdateTime {
			fv := rv.FieldByIndex(f.StructIdx)
			if fv.CanSet() {
				fv.Set(reflect.ValueOf(now).Convert(fv.Type()))
			}
		}
	}
}

func setPrimaryKey(rv reflect.Value, m *schema.Model, id int64) {
	if len(m.PrimaryKeys) == 0 {
		return
	}
	fv := rv.FieldByIndex(m.PrimaryKeys[0].StructIdx)
	if !fv.CanSet() {
		return
	}
	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fv.SetInt(id)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		fv.SetUint(uint64(id))
	}
}

// ─────────────────────────────────────────────────────────────
//  Logging
// ─────────────────────────────────────────────────────────────

func (c *Chain) logAndRecord(q string, args []interface{}, d time.Duration, op string, err error) {
	if c.logger != nil {
		c.logger.LogQuery(q, args, d, err)
	}
	c.recordQuery(q, args, d, op, err)
}

// ─────────────────────────────────────────────────────────────
//  Error helpers
// ─────────────────────────────────────────────────────────────

func errNotFound() error {
	return errRecordNotFound
}

var errRecordNotFound = fmt.Errorf("godb: record not found")

func errNoCondition() error {
	return fmt.Errorf("godb: Update/Delete requires at least one WHERE condition — use Unscoped() to bypass")
}

func wrapDBErr(err error, sqlStr string) error {
	return fmt.Errorf("godb [DB]: %w (sql: %s)", err, sqlStr)
}

// ensure context import is used
var _ context.Context
