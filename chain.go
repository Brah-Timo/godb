package godb

import (
	"github.com/Brah-Timo/godb/builder"
	"github.com/Brah-Timo/godb/schema"
)

// Chain is the fluent query builder returned by DB.Model(), DB.Table(), DB.Raw().
// It is an alias for builder.Chain so callers only need to import "github.com/Brah-Timo/godb".
type Chain = builder.Chain

// newChain creates a fully initialised builder Chain for the given schema model.
func newChain(db *DB, s *schema.Model) *Chain {
	return builder.New(
		db.sqlDB,
		db.dialect,
		s,
		db.cacheImpl,
		db.logger,
		db.collector,
		db.ctx,
		db.config.DryRun,
		db.config.NowFunc,
	)
}

// newChainTable creates a Chain for a raw table name (no struct model).
func newChainTable(db *DB, table string) *Chain {
	return builder.NewTable(
		db.sqlDB,
		db.dialect,
		table,
		db.cacheImpl,
		db.logger,
		db.collector,
		db.ctx,
		db.config.DryRun,
		db.config.NowFunc,
	)
}

// newErrChain creates a Chain that immediately returns err on execution.
func newErrChain(err error) *Chain {
	return builder.NewErr(err)
}
