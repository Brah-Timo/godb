package godb

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─────────────────────────────────────────────────────────────
//  Log levels
// ─────────────────────────────────────────────────────────────

// LogLevel controls query logging verbosity.
type LogLevel int32

const (
	// LogSilent disables all godb-emitted log output.
	LogSilent LogLevel = iota
	// LogError logs only errors.
	LogError
	// LogWarn logs errors and slow-query warnings. (default)
	LogWarn
	// LogInfo logs all executed queries with their duration.
	LogInfo
	// LogDebug logs everything including internal state changes.
	LogDebug
)

func (l LogLevel) String() string {
	switch l {
	case LogSilent:
		return "SILENT"
	case LogError:
		return "ERROR"
	case LogWarn:
		return "WARN"
	case LogInfo:
		return "INFO"
	case LogDebug:
		return "DEBUG"
	default:
		return "UNKNOWN"
	}
}

// ─────────────────────────────────────────────────────────────
//  Logger interface
// ─────────────────────────────────────────────────────────────

// Logger is the interface consumed by the builder, migrator, and cache layers.
// Implement it to integrate godb's logging into your own logging library
// (zap, zerolog, slog, logrus, …).
type Logger interface {
	// LogQuery is called after every SQL statement execution.
	LogQuery(sql string, args []interface{}, duration time.Duration, err error)
	// LogWarnf logs a warning-level formatted message.
	LogWarnf(format string, args ...interface{})
	// LogInfof logs an info-level formatted message.
	LogInfof(format string, args ...interface{})
	// LogDebugf logs a debug-level formatted message.
	LogDebugf(format string, args ...interface{})
	// SetLevel dynamically changes the logging level.
	SetLevel(level LogLevel)
	// Level returns the current logging level.
	Level() LogLevel
}

// ─────────────────────────────────────────────────────────────
//  Default logger (stdlib)
// ─────────────────────────────────────────────────────────────

// defaultLogger is godb's built-in structured logger backed by the standard
// library. It colourises output, highlights slow queries, and is goroutine-safe.
type defaultLogger struct {
	level     atomic.Int32
	threshold time.Duration
	out       *log.Logger
	errOut    *log.Logger
}

func newLogger(level LogLevel, slowThreshold time.Duration) *defaultLogger {
	if slowThreshold == 0 {
		slowThreshold = 200 * time.Millisecond
	}
	l := &defaultLogger{
		threshold: slowThreshold,
		out:       log.New(os.Stdout, "", 0),
		errOut:    log.New(os.Stderr, "", 0),
	}
	l.level.Store(int32(level))
	return l
}

// NewCustomLogger creates a defaultLogger writing to a custom io.Writer.
func NewCustomLogger(w io.Writer, level LogLevel, slowThreshold time.Duration) Logger {
	l := &defaultLogger{
		threshold: slowThreshold,
		out:       log.New(w, "", 0),
		errOut:    log.New(w, "", 0),
	}
	l.level.Store(int32(level))
	return l
}

func (l *defaultLogger) Level() LogLevel { return LogLevel(l.level.Load()) }

func (l *defaultLogger) SetLevel(lv LogLevel) { l.level.Store(int32(lv)) }

func (l *defaultLogger) LogQuery(sqlStr string, args []interface{}, d time.Duration, err error) {
	lv := l.Level()
	ms := float64(d.Microseconds()) / 1000.0

	if err != nil {
		if lv >= LogError {
			l.errOut.Printf("❌ [ERROR] [%.2fms] %s %v | error: %v", ms, truncateSQL(sqlStr), args, err)
		}
		return
	}

	slow := d >= l.threshold
	if slow && lv >= LogWarn {
		l.out.Printf("🐌 [SLOW %.2fms] %s %v", ms, truncateSQL(sqlStr), args)
		return
	}
	if lv >= LogInfo {
		l.out.Printf("✅ [%.2fms] %s %v", ms, truncateSQL(sqlStr), args)
	}
}

func (l *defaultLogger) LogWarnf(format string, args ...interface{}) {
	if l.Level() >= LogWarn {
		l.out.Printf("⚠️  [WARN] "+format, args...)
	}
}

func (l *defaultLogger) LogInfof(format string, args ...interface{}) {
	if l.Level() >= LogInfo {
		l.out.Printf("ℹ️  [INFO] "+format, args...)
	}
}

func (l *defaultLogger) LogDebugf(format string, args ...interface{}) {
	if l.Level() >= LogDebug {
		l.out.Printf("🔍 [DEBUG] "+format, args...)
	}
}

// truncateSQL shortens overly long SQL strings for log readability.
func truncateSQL(sqlStr string) string {
	sqlStr = strings.TrimSpace(sqlStr)
	if len(sqlStr) > 500 {
		return sqlStr[:500] + "…"
	}
	return sqlStr
}

// ─────────────────────────────────────────────────────────────
//  No-op logger
// ─────────────────────────────────────────────────────────────

// NopLogger discards all log output. Equivalent to WithLogger(LogSilent).
type NopLogger struct{}

func (NopLogger) LogQuery(_ string, _ []interface{}, _ time.Duration, _ error) {}
func (NopLogger) LogWarnf(_ string, _ ...interface{})                          {}
func (NopLogger) LogInfof(_ string, _ ...interface{})                          {}
func (NopLogger) LogDebugf(_ string, _ ...interface{})                         {}
func (NopLogger) SetLevel(_ LogLevel)                                          {}
func (NopLogger) Level() LogLevel                                              { return LogSilent }

// ─────────────────────────────────────────────────────────────
//  Functional-options logger adapter
// ─────────────────────────────────────────────────────────────

// LoggerFunc lets you build a Logger from a single function, useful for
// integrating with slog, zap, zerolog, etc.
type LoggerFunc struct {
	QueryFn func(sql string, args []interface{}, d time.Duration, err error)
	WarnFn  func(format string, args ...interface{})
	InfoFn  func(format string, args ...interface{})
	DebugFn func(format string, args ...interface{})
	lv      atomic.Int32
}

func (f *LoggerFunc) LogQuery(sqlStr string, args []interface{}, d time.Duration, err error) {
	if f.QueryFn != nil {
		f.QueryFn(sqlStr, args, d, err)
	}
}
func (f *LoggerFunc) LogWarnf(format string, args ...interface{}) {
	if f.WarnFn != nil {
		f.WarnFn(format, args...)
	}
}
func (f *LoggerFunc) LogInfof(format string, args ...interface{}) {
	if f.InfoFn != nil {
		f.InfoFn(format, args...)
	}
}
func (f *LoggerFunc) LogDebugf(format string, args ...interface{}) {
	if f.DebugFn != nil {
		f.DebugFn(format, args...)
	}
}
func (f *LoggerFunc) SetLevel(lv LogLevel) { f.lv.Store(int32(lv)) }
func (f *LoggerFunc) Level() LogLevel      { return LogLevel(f.lv.Load()) }

// ─────────────────────────────────────────────────────────────
//  Statement cache
// ─────────────────────────────────────────────────────────────

// statementCache caches prepared statements keyed by their SQL text.
// Uses sync.Map for lock-free concurrent reads.
type statementCache struct {
	db    *sql.DB
	stmts sync.Map // map[string]*sql.Stmt
}

func newStatementCache(db *sql.DB) *statementCache {
	return &statementCache{db: db}
}

// Prepare returns a cached *sql.Stmt, preparing it on first use.
func (c *statementCache) Prepare(ctx context.Context, query string) (*sql.Stmt, error) {
	if v, ok := c.stmts.Load(query); ok {
		return v.(*sql.Stmt), nil
	}
	// Not found: prepare and cache
	stmt, err := c.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	// Store atomically; discard duplicate if another goroutine raced us
	if existing, loaded := c.stmts.LoadOrStore(query, stmt); loaded {
		stmt.Close()
		return existing.(*sql.Stmt), nil
	}
	return stmt, nil
}

// Close closes all cached prepared statements.
func (c *statementCache) Close() {
	c.stmts.Range(func(_, v interface{}) bool {
		v.(*sql.Stmt).Close()
		return true
	})
}

// ensure fmt is used
var _ = fmt.Sprintf
