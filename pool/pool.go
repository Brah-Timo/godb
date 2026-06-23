// Package pool provides utilities for monitoring and tuning the underlying
// database connection pool exposed by database/sql.
package pool

import (
	"database/sql"
	"fmt"
	"time"
)

// Stats wraps sql.DBStats with helper methods.
type Stats struct {
	sql.DBStats
	CollectedAt time.Time
}

// String formats the pool stats as a human-readable string.
func (s Stats) String() string {
	return fmt.Sprintf(
		"Pool{open:%d/%d idle:%d inUse:%d waitCount:%d waitDuration:%s}",
		s.OpenConnections, s.MaxOpenConnections,
		s.Idle, s.InUse,
		s.WaitCount, s.WaitDuration,
	)
}

// Utilisation returns the fraction of open connections currently in use.
func (s Stats) Utilisation() float64 {
	if s.OpenConnections == 0 {
		return 0
	}
	return float64(s.InUse) / float64(s.OpenConnections)
}

// Collector periodically samples pool statistics and stores the history.
type Collector struct {
	db      *sql.DB
	history []Stats
	maxLen  int
}

// NewCollector creates a Collector that retains the last maxLen samples.
func NewCollector(db *sql.DB, maxLen int) *Collector {
	return &Collector{db: db, maxLen: maxLen}
}

// Sample captures the current pool stats.
func (c *Collector) Sample() Stats {
	s := Stats{DBStats: c.db.Stats(), CollectedAt: time.Now()}
	c.history = append(c.history, s)
	if len(c.history) > c.maxLen {
		c.history = c.history[1:]
	}
	return s
}

// History returns all collected samples in chronological order.
func (c *Collector) History() []Stats { return c.history }

// Current returns the most recent sample.
func (c *Collector) Current() Stats {
	if len(c.history) == 0 {
		return c.Sample()
	}
	return c.history[len(c.history)-1]
}
