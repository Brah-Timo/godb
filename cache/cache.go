// Package cache provides the pluggable query-result caching layer for godb.
//
// Two backends are included:
//   - memory: a thread-safe in-process LRU cache (default)
//   - redis:  a Redis-backed cache for multi-instance deployments
//
// Cache invalidation is table-scoped: any write on a table purges all cached
// SELECT results for that table.
//
// Usage:
//
//	godb.WithCache("memory", nil)           // 64 MB in-process LRU
//	godb.WithCache("memory", map[string]string{"max_size_mb": "256"})
//	godb.WithCache("redis", map[string]string{"addr": "localhost:6379"})
package cache

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"
)

// ─────────────────────────────────────────────────────────────
//  Cache interface
// ─────────────────────────────────────────────────────────────

// Cache is the unified interface for all godb cache backends.
// All methods must be goroutine-safe.
type Cache interface {
	// Get retrieves a cached value by key.
	// Returns (data, nil) on hit; returns ("", ErrCacheMiss) on miss.
	Get(key string) ([]byte, error)

	// Set stores value at key with the given TTL.
	Set(key string, value []byte, ttl time.Duration) error

	// Delete removes a single key from the cache.
	Delete(key string) error

	// InvalidateTable removes all cache entries associated with table.
	// Called automatically by Create/Update/Delete operations.
	InvalidateTable(table string) error

	// Flush clears the entire cache.
	Flush() error

	// Stats returns cache performance statistics.
	Stats() Stats

	// Close releases any resources held by the cache backend.
	Close() error
}

// Stats holds cache performance counters.
type Stats struct {
	Hits    int64
	Misses  int64
	Entries int64
	SizeKB  int64
}

// HitRate returns the cache hit rate as a percentage (0.0–100.0).
func (s Stats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total) * 100
}

// ─────────────────────────────────────────────────────────────
//  Factory
// ─────────────────────────────────────────────────────────────

// New creates a Cache backend based on the driver name and options.
//
// Supported drivers:
//   - "memory" — in-process LRU cache (options: "max_size_mb")
//   - "redis"  — Redis-backed cache (options: "addr", "password", "db")
func New(driver string, opts map[string]string) (Cache, error) {
	if opts == nil {
		opts = map[string]string{}
	}
	switch driver {
	case "memory":
		maxMB := int64(64)
		if v, ok := opts["max_size_mb"]; ok {
			_, err := fmt.Sscanf(v, "%d", &maxMB)
			if err != nil || maxMB <= 0 {
				maxMB = 64
			}
		}
		return NewMemoryCache(maxMB * 1024 * 1024), nil
	case "redis":
		addr := opts["addr"]
		if addr == "" {
			addr = "localhost:6379"
		}
		password := opts["password"]
		db := 0
		if v, ok := opts["db"]; ok {
			fmt.Sscanf(v, "%d", &db)
		}
		prefix := opts["prefix"]
		if prefix == "" {
			prefix = "godb"
		}
		return NewRedisCache(addr, password, db, prefix)
	default:
		return nil, fmt.Errorf("godb/cache: unsupported driver %q (use \"memory\" or \"redis\")", driver)
	}
}

// ─────────────────────────────────────────────────────────────
//  Key building
// ─────────────────────────────────────────────────────────────

// BuildKey creates a deterministic cache key for (table, sql, args).
// The table prefix is kept unobfuscated so InvalidateTable can delete by prefix.
func BuildKey(table, sql string, args []interface{}) string {
	payload, _ := json.Marshal(args)
	h := md5.Sum(append([]byte(sql), payload...))
	return fmt.Sprintf("godb:%s:%x", table, h)
}

// TablePrefix returns the cache key prefix for a given table.
// Used by InvalidateTable to find and delete all matching entries.
func TablePrefix(table string) string {
	return "godb:" + table + ":"
}
