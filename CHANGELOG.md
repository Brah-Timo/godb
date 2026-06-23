# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project adheres to Semantic Versioning.

## [1.0.0] - 2026-06-23

### Added

#### Core ORM
- Database-agnostic ORM for Go 1.22+
- Fluent immutable query builder
- CRUD operations (Create, Find, First, Last, Take, Update, Updates, Save, Delete)
- BulkCreate and BulkUpdate support
- Raw SQL execution and scanning
- Count, Exists, and Pluck helpers

#### Query Builder
- Where, OrWhere, Not filters
- WhereIn, WhereNotIn
- WhereBetween
- WhereNull and WhereNotNull
- Joins and LeftJoin
- Order, Limit, Offset
- GroupBy and Having
- SQL inspection via ToSQL()

#### Multi-Dialect Support
- PostgreSQL dialect
- MySQL / MariaDB dialect
- SQLite dialect
- Pluggable dialect registry

#### Schema & Models
- Struct-tag based schema definition
- Custom table naming
- Automatic field discovery
- Index and unique index generation
- Timestamp helpers
- Soft-delete field support

#### Migrations
- AutoMigrate support
- Migration planning engine
- Schema diff detection
- Migration history tracking
- Raw SQL migration execution

#### Relations
- HasMany support
- HasOne support
- BelongsTo support
- ManyToMany support
- Eager loading through Preload()

#### Hooks
- BeforeCreate
- AfterCreate
- BeforeUpdate
- AfterUpdate
- BeforeDelete
- AfterDelete
- Validator interface

#### Caching
- In-memory LRU cache
- Redis cache backend
- Table-based cache invalidation
- Query-level caching

#### Transactions
- Transaction helper
- Automatic rollback on errors
- Nested transaction support

#### Observability
- Structured logging
- Custom logger interface
- Query timing metrics
- Embedded dashboard
- Connection pool metrics

#### Testing
- Unit tests across builder, cache, migration, schema, and dialect packages
- Race-detector compatibility
- SQLite integration testing support

### Security
- WHERE-less UPDATE protection
- WHERE-less DELETE protection
- Typed database errors
- Duplicate-key detection
- Record-not-found detection

### Performance
- Prepared statement cache
- Connection pooling
- Query caching
- Immutable chain reuse

---

Initial public release of godb.
