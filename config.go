package godb

import "time"

// ─────────────────────────────────────────────────────────────
//  Config
// ─────────────────────────────────────────────────────────────

// Config holds every tunable knob for a godb.DB instance.
// You never create Config directly — use the With* functional options.
type Config struct {
	// ── Connection Pool ─────────────────────────────────────
	// MaxOpenConns is the maximum number of open connections to the database.
	MaxOpenConns int
	// MaxIdleConns is the maximum number of connections in the idle pool.
	MaxIdleConns int
	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	ConnMaxLifetime time.Duration
	// ConnMaxIdleTime is the maximum amount of time a connection may be idle.
	ConnMaxIdleTime time.Duration

	// ── Logging ─────────────────────────────────────────────
	// LogLevel controls the verbosity of the built-in logger.
	LogLevel LogLevel
	// SlowQueryThreshold defines the duration after which a query is flagged as slow.
	// Default: 200ms.
	SlowQueryThreshold time.Duration

	// ── Cache ───────────────────────────────────────────────
	// CacheDriver selects the cache backend: "memory", "redis", or "" (disabled).
	CacheDriver string
	// CacheOptions provides driver-specific settings.
	// For "memory": "max_size_mb" (default "64").
	// For "redis":  "addr", "password", "db".
	CacheOptions map[string]string

	// ── Dashboard (paid tier) ────────────────────────────────
	// DashboardEnabled starts the embedded HTTP dashboard server.
	DashboardEnabled bool
	// DashboardPort is the TCP port the dashboard listens on. Default: 8080.
	DashboardPort int
	// DashboardToken is the bearer token required to access the dashboard API.
	// Must be at least 16 characters.
	DashboardToken string

	// ── General behaviour ───────────────────────────────────
	// DryRun makes every query builder generate SQL without executing it.
	// The generated SQL is returned via Chain.ToSQL().
	DryRun bool
	// PrepareStmt enables automatic caching of prepared statements.
	// Drastically reduces per-query overhead for repeated queries.
	PrepareStmt bool
	// NowFunc overrides the time source used for AutoCreateTime / AutoUpdateTime.
	// Useful in tests.
	NowFunc func() time.Time
}

// defaultConfig returns sensible production-ready defaults.
func defaultConfig() *Config {
	return &Config{
		MaxOpenConns:       25,
		MaxIdleConns:       10,
		ConnMaxLifetime:    5 * time.Minute,
		ConnMaxIdleTime:    2 * time.Minute,
		LogLevel:           LogWarn,
		SlowQueryThreshold: 200 * time.Millisecond,
		PrepareStmt:        true,
		DashboardPort:      8080,
		NowFunc:            time.Now,
	}
}

// ─────────────────────────────────────────────────────────────
//  Functional Options
// ─────────────────────────────────────────────────────────────

// Option is a function that mutates a Config.
// Use the With* constructors below.
type Option func(*Config)

// WithPool configures the underlying sql.DB connection pool.
//
//	godb.WithPool(maxOpen, maxIdle int, connLifetime time.Duration)
func WithPool(maxOpen, maxIdle int, lifetime time.Duration) Option {
	return func(c *Config) {
		c.MaxOpenConns = maxOpen
		c.MaxIdleConns = maxIdle
		c.ConnMaxLifetime = lifetime
	}
}

// WithIdleTimeout sets how long a connection can remain idle before being closed.
func WithIdleTimeout(d time.Duration) Option {
	return func(c *Config) { c.ConnMaxIdleTime = d }
}

// WithCache enables query-level result caching.
//
// driver values: "memory" | "redis"
//
// Memory options:
//
//	map[string]string{"max_size_mb": "128"}
//
// Redis options:
//
//	map[string]string{"addr": "localhost:6379", "password": "", "db": "0"}
func WithCache(driver string, opts map[string]string) Option {
	return func(c *Config) {
		c.CacheDriver = driver
		c.CacheOptions = opts
	}
}

// WithLogger sets the logging verbosity.
// Levels: LogSilent, LogError, LogWarn, LogInfo, LogDebug.
func WithLogger(level LogLevel) Option {
	return func(c *Config) { c.LogLevel = level }
}

// WithSlowQueryAlert sets the duration threshold above which a query is
// flagged as "slow" in logs and the dashboard.
func WithSlowQueryAlert(threshold time.Duration) Option {
	return func(c *Config) { c.SlowQueryThreshold = threshold }
}

// WithDashboard enables the built-in monitoring dashboard.
// token must be at least 16 characters and is required to access the dashboard.
//
//	godb.WithDashboard(8080, "my-super-secret-token")
func WithDashboard(port int, token string) Option {
	return func(c *Config) {
		c.DashboardEnabled = true
		c.DashboardPort = port
		c.DashboardToken = token
	}
}

// WithDryRun enables dry-run mode: query builders generate SQL but never
// execute it. Inspect queries via Chain.ToSQL().
func WithDryRun() Option {
	return func(c *Config) { c.DryRun = true }
}

// WithPrepareStmt enables or disables prepared statement caching.
// Enabled by default.
func WithPrepareStmt(enabled bool) Option {
	return func(c *Config) { c.PrepareStmt = enabled }
}

// WithNowFunc overrides the time source (useful in unit tests).
//
//	godb.WithNowFunc(func() time.Time { return fixedTime })
func WithNowFunc(fn func() time.Time) Option {
	return func(c *Config) { c.NowFunc = fn }
}
