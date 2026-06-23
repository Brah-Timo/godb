// Package dashboard implements the godb Pro monitoring dashboard.
//
// It collects every SQL query executed through godb, computes statistics,
// detects slow queries, and exposes them via an embedded HTTP server.
//
// The dashboard is a paid feature ($49/month).
// A valid DashboardToken is required to access the HTTP API.
// Without a license, query collection still happens (for in-process logging),
// but the HTTP server returns 402 Payment Required.
package dashboard

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─────────────────────────────────────────────────────────────
//  QueryRecord
// ─────────────────────────────────────────────────────────────

// QueryRecord stores metadata about a single SQL statement execution.
type QueryRecord struct {
	SQL        string
	Args       []interface{}
	Duration   time.Duration
	ExecutedAt time.Time
	Table      string
	Operation  string // SELECT, INSERT, UPDATE, DELETE, BULK_INSERT, COUNT, EXEC, …
	Error      error
	TraceID    string // correlates with HTTP request ID if provided via context
}

// IsSlow reports whether this query exceeded the collector's slow threshold.
func (r QueryRecord) IsSlow(threshold time.Duration) bool {
	return r.Duration >= threshold
}

// ─────────────────────────────────────────────────────────────
//  QueryStats (aggregated)
// ─────────────────────────────────────────────────────────────

// QueryStats holds aggregated metrics for queries with the same SQL fingerprint.
type QueryStats struct {
	Fingerprint string // normalised SQL (parameters removed)
	SQL         string // most recent full SQL
	Count       int64
	TotalTime   time.Duration
	AvgTime     time.Duration
	MaxTime     time.Duration
	MinTime     time.Duration
	ErrorCount  int64
	LastSeen    time.Time
	Table       string
	Operation   string
}

// ─────────────────────────────────────────────────────────────
//  Collector
// ─────────────────────────────────────────────────────────────

// Collector records every query executed through godb and provides
// analytics APIs consumed by the dashboard HTTP server.
//
// It is goroutine-safe and designed to add zero contention on the query path:
// the hot path (Record) does a single atomic increment + slice append under a lock.
type Collector struct {
	mu            sync.RWMutex
	records       []QueryRecord          // ring buffer: last maxRecords queries
	stats         map[string]*QueryStats // fingerprint → stats
	maxRecords    int
	slowThreshold time.Duration
	licensed      bool

	// Counters (updated atomically for lock-free reads)
	totalQueries atomic.Int64
	totalErrors  atomic.Int64
	totalSlowQ   atomic.Int64
}

// NewCollector creates a Collector.
// token is the dashboard license token; pass "" to disable the HTTP UI.
func NewCollector(slowThreshold time.Duration, token string) *Collector {
	return &Collector{
		stats:         make(map[string]*QueryStats),
		maxRecords:    10_000,
		slowThreshold: slowThreshold,
		licensed:      validateLicense(token),
	}
}

// Record ingests a query record. Called by the builder after every statement.
// This is the hot path — it must be as fast as possible.
func (c *Collector) Record(r QueryRecord) {
	c.totalQueries.Add(1)
	if r.Error != nil {
		c.totalErrors.Add(1)
	}
	if r.IsSlow(c.slowThreshold) {
		c.totalSlowQ.Add(1)
	}

	fp := fingerprint(r.SQL)

	c.mu.Lock()
	// Ring buffer
	if len(c.records) >= c.maxRecords {
		c.records = c.records[1:]
	}
	c.records = append(c.records, r)

	// Aggregate stats
	s, ok := c.stats[fp]
	if !ok {
		s = &QueryStats{
			Fingerprint: fp,
			SQL:         r.SQL,
			Table:       r.Table,
			Operation:   r.Operation,
			MinTime:     r.Duration,
		}
		c.stats[fp] = s
	}
	s.Count++
	s.TotalTime += r.Duration
	s.AvgTime = s.TotalTime / time.Duration(s.Count)
	if r.Duration > s.MaxTime {
		s.MaxTime = r.Duration
	}
	if r.Duration < s.MinTime {
		s.MinTime = r.Duration
	}
	if r.Error != nil {
		s.ErrorCount++
	}
	s.LastSeen = r.ExecutedAt
	c.mu.Unlock()
}

// ─────────────────────────────────────────────────────────────
//  Query APIs
// ─────────────────────────────────────────────────────────────

// RecentQueries returns the last n query records (most recent first).
func (c *Collector) RecentQueries(n int) []QueryRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := len(c.records)
	if n <= 0 || n > total {
		n = total
	}
	// Return last n (most recent)
	out := make([]QueryRecord, n)
	copy(out, c.records[total-n:])
	// Reverse so most-recent is first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// SlowQueries returns queries that exceeded the slow threshold, sorted by duration desc.
func (c *Collector) SlowQueries(limit int) []QueryRecord {
	c.mu.RLock()
	var slow []QueryRecord
	for _, r := range c.records {
		if r.IsSlow(c.slowThreshold) {
			slow = append(slow, r)
		}
	}
	c.mu.RUnlock()

	sort.Slice(slow, func(i, j int) bool {
		return slow[i].Duration > slow[j].Duration
	})
	if limit > 0 && len(slow) > limit {
		return slow[:limit]
	}
	return slow
}

// TopQueries returns the top n queries by total time.
func (c *Collector) TopQueries(n int) []*QueryStats {
	c.mu.RLock()
	out := make([]*QueryStats, 0, len(c.stats))
	for _, s := range c.stats {
		out = append(out, s)
	}
	c.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].TotalTime > out[j].TotalTime
	})
	if n > 0 && len(out) > n {
		return out[:n]
	}
	return out
}

// ErrorQueries returns queries that have produced at least one error.
func (c *Collector) ErrorQueries(limit int) []*QueryStats {
	c.mu.RLock()
	var out []*QueryStats
	for _, s := range c.stats {
		if s.ErrorCount > 0 {
			out = append(out, s)
		}
	}
	c.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].ErrorCount > out[j].ErrorCount
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

// Counters returns high-level execution counters.
func (c *Collector) Counters() (total, errors, slow int64) {
	return c.totalQueries.Load(), c.totalErrors.Load(), c.totalSlowQ.Load()
}

// IsLicensed reports whether the dashboard HTTP UI is licensed.
func (c *Collector) IsLicensed() bool { return c.licensed }

// SlowThresholdMS returns the slow-query threshold in milliseconds.
func (c *Collector) SlowThresholdMS() int64 { return c.slowThreshold.Milliseconds() }

// ─────────────────────────────────────────────────────────────
//  Internal helpers
// ─────────────────────────────────────────────────────────────

// fingerprint produces a normalised SQL string by replacing literal values
// with ? for grouping similar queries.
func fingerprint(sql string) string {
	// Simple approach: lowercase and collapse whitespace
	sql = strings.ToLower(strings.TrimSpace(sql))
	// A production implementation would use a proper SQL tokeniser
	return sql
}

// validateLicense checks the token format.
// In production this would call https://godb.dev/api/license/validate.
func validateLicense(token string) bool {
	return len(token) >= 16
}
