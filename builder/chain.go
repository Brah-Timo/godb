// Package builder contains the fluent query chain (Chain) that godb exposes
// as its primary API surface.
//
// Design principles:
//   - Every method returns a new *Chain (copy-on-write) → goroutine-safe.
//   - SQL is assembled in a single strings.Builder pass per execution → minimal allocations.
//   - Placeholders are normalised at the last possible moment by the dialect.
//   - Cache integration is transparent: callers just call .Cache(ttl).
package builder

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"time"

	"github.com/Brah-Timo/godb/cache"
	"github.com/Brah-Timo/godb/dashboard"
	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/schema"
)

// ─────────────────────────────────────────────────────────────
//  sqlExecutor — minimal DB interface used by the chain
//  (satisfied by *sql.DB and the txSQLDB shim in godb.go)
// ─────────────────────────────────────────────────────────────

// sqlExecutor is the minimal interface the chain requires.
// It is identical to godb.SQLDB to avoid an import cycle.
type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	PingContext(ctx context.Context) error
	Stats() sql.DBStats
	Close() error
}

// ─────────────────────────────────────────────────────────────
//  Chain
// ─────────────────────────────────────────────────────────────

// Chain is an immutable query builder. Every method call returns a new copy;
// the original is never modified. Call Find/First/Last/Count/Create/Updates/Delete
// to execute.
type Chain struct {
	db        sqlExecutor // *sql.DB or txSQLDB shim
	dialect   dialects.Dialect
	model     *schema.Model
	cacheImpl cache.Cache
	logger    logger
	collector *dashboard.Collector
	ctx       context.Context
	dryRun    bool
	nowFn     func() time.Time

	// Query parts
	table     string
	selects   []string
	wheres    []whereClause
	orWheres  []whereClause // separated for grouping clarity
	joins     []joinClause
	orderBys  []string
	groupBys  []string
	havings   []whereClause
	limit     int // 0 = not set
	offset    int // 0 = not set
	preloads  []string
	distinct  bool
	forUpdate bool
	unscoped  bool // bypass soft-delete filter

	// Cache
	cacheTTL time.Duration

	// Raw SQL mode
	rawSQL  string
	rawArgs []interface{}

	// Deferred error (set by constructors on parse failure)
	err error

	// Accumulated placeholder arg counter (per dialect)
	argCount int
}

// logger interface (matches godb.Logger, redefined here to avoid import cycle)
type logger interface {
	LogQuery(sql string, args []interface{}, d time.Duration, err error)
	LogWarnf(format string, args ...interface{})
	LogInfof(format string, args ...interface{})
	LogDebugf(format string, args ...interface{})
}

// ─────────────────────────────────────────────────────────────
//  Constructors (called from godb package)
// ─────────────────────────────────────────────────────────────

// New creates a Chain bound to a schema Model.
func New(
	db sqlExecutor,
	d dialects.Dialect,
	m *schema.Model,
	c cache.Cache,
	l logger,
	col *dashboard.Collector,
	ctx context.Context,
	dryRun bool,
	nowFn func() time.Time,
) *Chain {
	return &Chain{
		db:        db,
		dialect:   d,
		model:     m,
		cacheImpl: c,
		logger:    l,
		collector: col,
		ctx:       ctx,
		dryRun:    dryRun,
		nowFn:     nowFn,
		table:     m.Table,
	}
}

// NewTable creates a Chain bound to a raw table name (no struct model).
func NewTable(
	db sqlExecutor,
	d dialects.Dialect,
	table string,
	c cache.Cache,
	l logger,
	col *dashboard.Collector,
	ctx context.Context,
	dryRun bool,
	nowFn func() time.Time,
) *Chain {
	return &Chain{
		db:        db,
		dialect:   d,
		cacheImpl: c,
		logger:    l,
		collector: col,
		ctx:       ctx,
		dryRun:    dryRun,
		nowFn:     nowFn,
		table:     table,
	}
}

// NewErr creates a Chain that immediately returns err on any execution.
func NewErr(err error) *Chain { return &Chain{err: err} }

// ─────────────────────────────────────────────────────────────
//  Copy-on-write helper
// ─────────────────────────────────────────────────────────────

// clone creates a shallow copy of c with all slices deep-copied.
func (c *Chain) clone() *Chain {
	nc := *c
	nc.selects = append([]string(nil), c.selects...)
	nc.wheres = append([]whereClause(nil), c.wheres...)
	nc.orWheres = append([]whereClause(nil), c.orWheres...)
	nc.joins = append([]joinClause(nil), c.joins...)
	nc.orderBys = append([]string(nil), c.orderBys...)
	nc.groupBys = append([]string(nil), c.groupBys...)
	nc.havings = append([]whereClause(nil), c.havings...)
	nc.preloads = append([]string(nil), c.preloads...)
	return &nc
}

// ─────────────────────────────────────────────────────────────
//  Clause types
// ─────────────────────────────────────────────────────────────

type whereClause struct {
	query string
	args  []interface{}
}

type joinClause struct {
	kind      string // INNER / LEFT / RIGHT / CROSS
	table     string
	condition string
	args      []interface{}
}

// ─────────────────────────────────────────────────────────────
//  Fluent setters — all return *Chain (new copy)
// ─────────────────────────────────────────────────────────────

// Select specifies which columns to include in the SELECT list.
//
//	db.Model(&User{}).Select("id", "name", "email").Find(&users)
//	db.Model(&User{}).Select("users.*, COUNT(posts.id) AS post_count").Find(&users)
func (c *Chain) Select(cols ...string) *Chain {
	nc := c.clone()
	nc.selects = cols
	return nc
}

// Where adds an AND WHERE condition.
//
//	.Where("age > ?", 18)
//	.Where("name LIKE ?", "%alice%")
func (c *Chain) Where(query string, args ...interface{}) *Chain {
	nc := c.clone()
	nc.wheres = append(nc.wheres, whereClause{query: query, args: args})
	return nc
}

// OrWhere adds an OR WHERE condition (grouped: … AND (prev OR new)).
func (c *Chain) OrWhere(query string, args ...interface{}) *Chain {
	nc := c.clone()
	nc.orWheres = append(nc.orWheres, whereClause{query: query, args: args})
	return nc
}

// Not adds an AND NOT WHERE condition.
func (c *Chain) Not(query string, args ...interface{}) *Chain {
	return c.Where("NOT ("+query+")", args...)
}

// WhereIn adds an AND WHERE col IN (values…) condition.
func (c *Chain) WhereIn(col string, values ...interface{}) *Chain {
	if len(values) == 0 {
		return c.Where("1 = 0") // no matches possible
	}
	placeholders := makePlaceholders(len(values))
	return c.Where(c.dialect.QuoteIdent(col)+" IN ("+placeholders+")", values...)
}

// WhereNotIn adds an AND WHERE col NOT IN (values…) condition.
func (c *Chain) WhereNotIn(col string, values ...interface{}) *Chain {
	if len(values) == 0 {
		return c
	}
	placeholders := makePlaceholders(len(values))
	return c.Where(c.dialect.QuoteIdent(col)+" NOT IN ("+placeholders+")", values...)
}

// WhereBetween adds AND WHERE col BETWEEN lo AND hi.
func (c *Chain) WhereBetween(col string, lo, hi interface{}) *Chain {
	return c.Where(c.dialect.QuoteIdent(col)+" BETWEEN ? AND ?", lo, hi)
}

// WhereNull adds AND WHERE col IS NULL.
func (c *Chain) WhereNull(col string) *Chain {
	return c.Where(c.dialect.QuoteIdent(col) + " IS NULL")
}

// WhereNotNull adds AND WHERE col IS NOT NULL.
func (c *Chain) WhereNotNull(col string) *Chain {
	return c.Where(c.dialect.QuoteIdent(col) + " IS NOT NULL")
}

// Joins adds an INNER JOIN.
//
//	.Joins("posts", "posts.user_id = users.id")
func (c *Chain) Joins(table, condition string, args ...interface{}) *Chain {
	nc := c.clone()
	nc.joins = append(nc.joins, joinClause{kind: "INNER", table: table, condition: condition, args: args})
	return nc
}

// LeftJoin adds a LEFT JOIN.
func (c *Chain) LeftJoin(table, condition string, args ...interface{}) *Chain {
	nc := c.clone()
	nc.joins = append(nc.joins, joinClause{kind: "LEFT", table: table, condition: condition, args: args})
	return nc
}

// RightJoin adds a RIGHT JOIN.
func (c *Chain) RightJoin(table, condition string, args ...interface{}) *Chain {
	nc := c.clone()
	nc.joins = append(nc.joins, joinClause{kind: "RIGHT", table: table, condition: condition, args: args})
	return nc
}

// Order adds an ORDER BY clause.
//
//	.Order("created_at DESC")
//	.Order("name ASC").Order("age DESC")
func (c *Chain) Order(order string) *Chain {
	nc := c.clone()
	nc.orderBys = append(nc.orderBys, order)
	return nc
}

// Limit sets the maximum number of rows returned.
func (c *Chain) Limit(n int) *Chain {
	nc := c.clone()
	nc.limit = n
	return nc
}

// Offset sets the number of rows to skip.
func (c *Chain) Offset(n int) *Chain {
	nc := c.clone()
	nc.offset = n
	return nc
}

// GroupBy adds GROUP BY columns.
func (c *Chain) GroupBy(cols ...string) *Chain {
	nc := c.clone()
	nc.groupBys = append(nc.groupBys, cols...)
	return nc
}

// Having adds a HAVING condition (requires GroupBy).
func (c *Chain) Having(query string, args ...interface{}) *Chain {
	nc := c.clone()
	nc.havings = append(nc.havings, whereClause{query: query, args: args})
	return nc
}

// Distinct adds SELECT DISTINCT.
func (c *Chain) Distinct() *Chain {
	nc := c.clone()
	nc.distinct = true
	return nc
}

// ForUpdate adds SELECT … FOR UPDATE (locks selected rows).
func (c *Chain) ForUpdate() *Chain {
	nc := c.clone()
	nc.forUpdate = true
	return nc
}

// Cache enables result caching for this query with the given TTL.
// Results are keyed by the full SQL + args fingerprint.
//
//	.Cache(5 * time.Minute)
func (c *Chain) Cache(ttl time.Duration) *Chain {
	nc := c.clone()
	nc.cacheTTL = ttl
	return nc
}

// Preload requests eager loading of the named relation.
// Multiple Preload calls may be chained.
//
//	.Preload("Posts").Preload("Posts.Comments")
func (c *Chain) Preload(relation string) *Chain {
	nc := c.clone()
	nc.preloads = append(nc.preloads, relation)
	return nc
}

// Unscoped disables the automatic soft-delete filter so that
// deleted records are included in results.
func (c *Chain) Unscoped() *Chain {
	nc := c.clone()
	nc.unscoped = true
	return nc
}

// Raw sets a raw SQL query on the chain.
// Use Scan() or Find() to execute.
func (c *Chain) Raw(sqlStr string, args ...interface{}) *Chain {
	nc := c.clone()
	nc.rawSQL = sqlStr
	nc.rawArgs = args
	return nc
}

// Table overrides the table name for this chain.
func (c *Chain) Table(name string) *Chain {
	nc := c.clone()
	nc.table = name
	return nc
}

// ─────────────────────────────────────────────────────────────
//  ToSQL — introspection
// ─────────────────────────────────────────────────────────────

// ToSQL returns the SQL string and bound arguments without executing.
// Useful with DryRun mode or for logging / auditing.
func (c *Chain) ToSQL() (string, []interface{}) {
	if c.rawSQL != "" {
		return c.rawSQL, c.rawArgs
	}
	return buildSelect(c)
}

// ─────────────────────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────────────────────

func makePlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	buf := make([]byte, n*2-1)
	for i := range buf {
		if i%2 == 0 {
			buf[i] = '?'
		} else {
			buf[i] = ','
		}
	}
	return string(buf)
}

// now returns the current time using the chain's nowFn or time.Now().
func (c *Chain) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

// checkErr returns c.err if set, used as early-exit guard.
func (c *Chain) checkErr() error { return c.err }

// isZero reports whether v is the zero value for its type.
func isZero(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return schema.IsZeroValue(rv)
}

// recordQuery sends a query record to the collector (dashboard).
func (c *Chain) recordQuery(sqlStr string, args []interface{}, d time.Duration, op string, err error) {
	if c.collector == nil {
		return
	}
	c.collector.Record(dashboard.QueryRecord{
		SQL:        sqlStr,
		Args:       args,
		Duration:   d,
		ExecutedAt: time.Now(),
		Table:      c.table,
		Operation:  op,
		Error:      err,
	})
}

// tableForModel returns the table name, preferring c.table over c.model.Table.
func (c *Chain) tableForModel() string {
	if c.table != "" {
		return c.table
	}
	if c.model != nil {
		return c.model.Table
	}
	return ""
}

// ─────────────────────────────────────────────────────────────
//  Compile-time placeholder count helper
// ─────────────────────────────────────────────────────────────

// argCounter tracks argument position for dialect placeholder replacement.
type argCounter struct {
	n int
}

func (ac *argCounter) next() int {
	ac.n++
	return ac.n
}

// replacePlaceholders replaces ? in sql using the dialect's scheme,
// starting at ac.n+1 and advancing ac accordingly.
func (c *Chain) replacePlaceholders(sqlStr string, ac *argCounter, argCnt int) string {
	if argCnt == 0 {
		return sqlStr
	}
	result := c.dialect.ReplacePlaceholders(sqlStr, ac.n+1)
	ac.n += argCnt
	return result
}

// Ensure isZero is used (avoid "declared and not used" if only used in tests)
var _ = isZero
var _ = fmt.Sprintf
