// Package godb is a high-performance ORM for Go — Prisma-like API, 5x faster than GORM.
//
// Philosophy: You write Go, godb writes SQL.
//
// Quick start:
//
//	db, err := godb.Open("postgres", dsn,
//	    godb.WithPool(25, 10, 5*time.Minute),
//	    godb.WithCache("memory", nil),
//	    godb.WithLogger(godb.LogInfo),
//	)
//	defer db.Close()
//
//	db.AutoMigrate(&User{}, &Post{})
//
//	var users []User
//	db.Model(&User{}).Where("age > ?", 18).Order("name ASC").Limit(10).Find(&users)
package godb

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/Brah-Timo/godb/cache"
	"github.com/Brah-Timo/godb/dashboard"
	"github.com/Brah-Timo/godb/dialects"
	"github.com/Brah-Timo/godb/migration"
	"github.com/Brah-Timo/godb/schema"

	// Side-effect imports for auto-registering dialects
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// Version is the current godb semantic version.
const Version = "1.0.0"

// SQLDB is the executor interface used internally (satisfied by *sql.DB and *txSQLDB).
// Defined first so DB.sqlDB can reference it.
type SQLDB interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	PingContext(ctx context.Context) error
	Stats() sql.DBStats
	Close() error
}

// Ensure *sql.DB satisfies SQLDB at compile time.
var _ SQLDB = (*sql.DB)(nil)

// DB is the central gateway for all database operations.
// It is safe for concurrent use by multiple goroutines.
// It follows the Façade pattern: one entry point, zero boilerplate.
//
// Create via Open(), never instantiate directly.
type DB struct {
	sqlDB     SQLDB // *sql.DB normally, *txSQLDB during transactions
	dialect   dialects.Dialect
	cacheImpl cache.Cache
	logger    Logger
	config    *Config
	registry  *schema.Registry
	collector *dashboard.Collector
	stmtCache *statementCache
	mu        sync.RWMutex
	ctx       context.Context
	closed    bool
}

// Open opens a new database connection and returns a ready-to-use *DB.
//
// driver values: "postgres", "mysql", "sqlite3"
//
// Example DSNs:
//
//	postgres: "host=localhost user=app password=secret dbname=mydb sslmode=disable"
//	mysql:    "app:secret@tcp(localhost:3306)/mydb?parseTime=true"
//	sqlite3:  "file:mydb.sqlite?cache=shared&mode=rwc"
func Open(driver, dsn string, opts ...Option) (*DB, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	rawDB, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("godb: open connection: %w", err)
	}

	// Validate connection immediately
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rawDB.PingContext(ctx); err != nil {
		rawDB.Close()
		return nil, fmt.Errorf("godb: ping failed: %w", err)
	}

	// Resolve dialect
	d, err := dialects.Get(driver)
	if err != nil {
		rawDB.Close()
		return nil, err
	}

	// Configure connection pool
	rawDB.SetMaxOpenConns(cfg.MaxOpenConns)
	rawDB.SetMaxIdleConns(cfg.MaxIdleConns)
	rawDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	rawDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	db := &DB{
		sqlDB:    rawDB,
		dialect:  d,
		config:   cfg,
		registry: schema.NewRegistry(),
		logger:   newLogger(cfg.LogLevel, cfg.SlowQueryThreshold),
		ctx:      context.Background(),
	}

	// Initialise prepared statement cache
	if cfg.PrepareStmt {
		db.stmtCache = newStatementCache(rawDB)
	}

	// Initialise query cache
	if cfg.CacheDriver != "" {
		var cErr error
		db.cacheImpl, cErr = cache.New(cfg.CacheDriver, cfg.CacheOptions)
		if cErr != nil {
			rawDB.Close()
			return nil, fmt.Errorf("godb: cache init: %w", cErr)
		}
	}

	// Initialise dashboard collector (always; UI needs license)
	db.collector = dashboard.NewCollector(cfg.SlowQueryThreshold, cfg.DashboardToken)
	if cfg.DashboardEnabled {
		srv := dashboard.NewServer(db.collector, cfg.DashboardToken)
		if sErr := srv.Start(cfg.DashboardPort); sErr != nil {
			// Dashboard failure is non-fatal; log and continue
			db.logger.LogWarnf("godb: dashboard failed to start: %v", sErr)
		}
	}

	return db, nil
}

// MustOpen is like Open but panics on error. Useful in main() initialisation.
func MustOpen(driver, dsn string, opts ...Option) *DB {
	db, err := Open(driver, dsn, opts...)
	if err != nil {
		panic(fmt.Sprintf("godb.MustOpen: %v", err))
	}
	return db
}

// ─────────────────────────────────────────────────────────────
//  Context & scoping
// ─────────────────────────────────────────────────────────────

// WithContext returns a shallow copy of DB bound to the given context.
// Every query on the returned DB respects ctx's deadline / cancellation.
func (db *DB) WithContext(ctx context.Context) *DB {
	if ctx == nil {
		panic("godb: nil context passed to WithContext")
	}
	return db.shallowCopy(ctx, db.stmtCache)
}

// Session returns a new independent Session (scoped DB) that does NOT share
// prepared statement cache state, useful for per-request scoping.
func (db *DB) Session() *DB {
	var sc *statementCache
	if db.config.PrepareStmt {
		if rawDB, ok := db.sqlDB.(*sql.DB); ok {
			sc = newStatementCache(rawDB)
		}
	}
	return db.shallowCopy(db.ctx, sc)
}

// shallowCopy creates a new *DB sharing all fields with db but with independent
// ctx and stmtCache. It does NOT copy the mutex — each *DB owns its own lock.
func (db *DB) shallowCopy(ctx context.Context, sc *statementCache) *DB {
	return &DB{
		sqlDB:     db.sqlDB,
		dialect:   db.dialect,
		cacheImpl: db.cacheImpl,
		logger:    db.logger,
		config:    db.config,
		registry:  db.registry,
		collector: db.collector,
		stmtCache: sc,
		ctx:       ctx,
		// mu and closed intentionally zero-value (fresh per copy)
	}
}

// ─────────────────────────────────────────────────────────────
//  Model & query entry-points
// ─────────────────────────────────────────────────────────────

// Model starts a query chain scoped to the struct type of value.
// value can be a pointer to struct or pointer to slice.
//
//	db.Model(&User{}).Where("active = ?", true).Find(&users)
func (db *DB) Model(value interface{}) *Chain {
	s, err := db.registry.Parse(value)
	if err != nil {
		// Return a chain that immediately fails on execution
		return newErrChain(err)
	}
	return newChain(db, s)
}

// Table starts a raw-table query chain without a Go struct.
// Useful for join tables or queries returning ad-hoc structs.
//
//	db.Table("audit_logs").Where("user_id = ?", id).Scan(&logs)
func (db *DB) Table(name string) *Chain {
	return newChainTable(db, name)
}

// Raw creates a chain pre-loaded with raw SQL.
//
//	db.Raw("SELECT * FROM users WHERE id = ?", 1).Scan(&user)
func (db *DB) Raw(sqlStr string, args ...interface{}) *Chain {
	return newChainTable(db, "").Raw(sqlStr, args...)
}

// Exec executes a raw SQL statement that does not return rows (INSERT, UPDATE, DELETE, DDL).
func (db *DB) Exec(sqlStr string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	res, err := db.sqlDB.ExecContext(db.ctx, sqlStr, args...)
	elapsed := time.Since(start)
	db.logger.LogQuery(sqlStr, args, elapsed, err)
	db.collector.Record(dashboard.QueryRecord{
		SQL:        sqlStr,
		Args:       args,
		Duration:   elapsed,
		ExecutedAt: start,
		Operation:  "EXEC",
		Error:      err,
	})
	if err != nil {
		return nil, wrapError(err, sqlStr)
	}
	return res, nil
}

// ─────────────────────────────────────────────────────────────
//  Transaction
// ─────────────────────────────────────────────────────────────

// Transaction runs fn inside a database transaction.
// If fn returns nil the transaction is committed, otherwise it is rolled back.
//
//	err := db.Transaction(func(tx *godb.DB) error {
//	    if err := tx.Model(&User{}).Create(&user); err != nil {
//	        return err  // auto-rollback
//	    }
//	    return tx.Model(&Post{}).Create(&post)
//	})
func (db *DB) Transaction(fn func(tx *DB) error) error {
	return db.TransactionWithOpts(nil, fn)
}

// TransactionWithOpts is like Transaction but accepts custom TxOptions.
func (db *DB) TransactionWithOpts(opts *sql.TxOptions, fn func(tx *DB) error) error {
	rawDB, ok := db.sqlDB.(*sql.DB)
	if !ok {
		return fmt.Errorf("godb: cannot begin transaction on a transaction DB")
	}

	sqlTx, err := rawDB.BeginTx(db.ctx, opts)
	if err != nil {
		return fmt.Errorf("godb: begin transaction: %w", err)
	}

	txDB := db.Session()
	txDB.sqlDB = &txSQLDB{tx: sqlTx}

	defer func() {
		if p := recover(); p != nil {
			_ = sqlTx.Rollback()
			panic(p) // re-panic after rollback
		}
	}()

	if fnErr := fn(txDB); fnErr != nil {
		_ = sqlTx.Rollback()
		return fnErr
	}

	if commitErr := sqlTx.Commit(); commitErr != nil {
		return fmt.Errorf("godb: commit transaction: %w", commitErr)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
//  Migration
// ─────────────────────────────────────────────────────────────

// AutoMigrate compares the current database schema against the provided
// model structs and applies the minimum set of changes (CREATE TABLE,
// ADD COLUMN, CREATE INDEX). It never drops columns or tables.
//
//	db.AutoMigrate(&User{}, &Post{}, &Tag{})
func (db *DB) AutoMigrate(models ...interface{}) error {
	rawDB, ok := db.sqlDB.(*sql.DB)
	if !ok {
		return fmt.Errorf("godb: AutoMigrate requires a real *sql.DB, not a transaction")
	}
	m := migration.NewMigrator(rawDB, db.dialect, db.registry)
	return m.Run(db.ctx, models...)
}

// MigratePlan returns the SQL statements that AutoMigrate would execute,
// without actually running them. Use this to preview migrations.
func (db *DB) MigratePlan(models ...interface{}) (*migration.Plan, error) {
	rawDB, ok := db.sqlDB.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("godb: MigratePlan requires a real *sql.DB, not a transaction")
	}
	m := migration.NewMigrator(rawDB, db.dialect, db.registry)
	return m.Plan(db.ctx, models...)
}

// ─────────────────────────────────────────────────────────────
//  Utilities
// ─────────────────────────────────────────────────────────────

// Ping verifies that the database connection is still alive.
func (db *DB) Ping() error {
	return db.sqlDB.PingContext(db.ctx)
}

// Stats returns connection pool statistics.
func (db *DB) Stats() sql.DBStats {
	return db.sqlDB.Stats()
}

// Close closes all database connections gracefully.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil
	}
	db.closed = true
	if db.cacheImpl != nil {
		_ = db.cacheImpl.Close()
	}
	return db.sqlDB.Close()
}

// ─────────────────────────────────────────────────────────────
//  Internal helpers
// ─────────────────────────────────────────────────────────────

// txSQLDB is a thin shim that exposes *sql.Tx as an SQLDB-compatible surface.
type txSQLDB struct{ tx *sql.Tx }

func (t *txSQLDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}
func (t *txSQLDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}
func (t *txSQLDB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}
func (t *txSQLDB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, query)
}
func (t *txSQLDB) PingContext(_ context.Context) error { return nil }
func (t *txSQLDB) Stats() sql.DBStats                  { return sql.DBStats{} }
func (t *txSQLDB) Close() error                        { return nil }

// Ensure *txSQLDB satisfies SQLDB at compile time.
var _ SQLDB = (*txSQLDB)(nil)
