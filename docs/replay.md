# Replay (Applier)

**Package:** `internal/replay`
**File:** `applier.go`

## Overview

The Applier is the write-side of the pgmigrator pipeline. It consumes `Message` objects from a channel and applies the corresponding DML operations (INSERT, UPDATE, DELETE) to the destination PostgreSQL database. It maintains transaction boundaries, caches relation metadata, and provides an LSN confirmation callback that drives the replication slot advancement.

## Architecture

```
chan Message ──► Applier.Start()
                    │
                    ├── RelationMessage → cache schema
                    ├── BeginMessage    → BEGIN transaction
                    ├── ChangeMessage   → INSERT/UPDATE/DELETE
                    ├── CommitMessage   → COMMIT + callback
                    └── SentinelMessage → (handled by coordinator)
                              │
                              ▼
                    Destination PostgreSQL
```

## Types

### `Applier`

```go
type Applier struct {
    pool      *pgxpool.Pool                    // Destination connection pool
    logger    zerolog.Logger                    // Component-tagged logger
    mu        sync.Mutex                        // Protects lastLSN
    lastLSN   pglogrepl.LSN                    // Most recently committed LSN
    relations map[uint32]*stream.RelationMessage // Cached relation schemas
}
```

### `OnApplied`

```go
type OnApplied func(lsn pglogrepl.LSN)
```

Callback invoked after every successful COMMIT. The pipeline uses this to:
1. Advance the decoder's confirmed LSN
2. Update the metrics collector

## Construction

```go
applier := replay.NewApplier(destPool, logger)
```

Creates an applier with an empty relation cache and the logger tagged as `component: applier`.

## Message Processing (`Start`)

```go
func (a *Applier) Start(ctx context.Context, messages <-chan stream.Message, onApplied OnApplied) error
```

Blocks on the message channel and processes each message type:

### `RelationMessage`

```go
case *stream.RelationMessage:
    a.relations[m.RelationID] = m
```

Caches the schema metadata (column names, data types) keyed by `RelationID`. This cache is consulted by UPDATE and DELETE operations to build WHERE clauses.

### `BeginMessage`

```go
case *stream.BeginMessage:
    tx, err = a.pool.Begin(ctx)
```

Opens a new transaction on the destination. The transaction remains open until the corresponding `CommitMessage`.

### `ChangeMessage`

Dispatches to the appropriate DML method based on the operation type:

```go
case *stream.ChangeMessage:
    switch m.Op {
    case stream.OpInsert:
        err = a.applyInsert(ctx, tx, m)
    case stream.OpUpdate:
        err = a.applyUpdate(ctx, tx, m)
    case stream.OpDelete:
        err = a.applyDelete(ctx, tx, m)
    }
```

If any DML fails:
1. The current transaction is rolled back
2. `Start()` returns the error (pipeline handles recovery)

Messages received outside of a transaction (no prior `BeginMessage`) are logged as warnings and skipped.

### `CommitMessage`

```go
case *stream.CommitMessage:
    tx.Commit(ctx)
    a.lastLSN = m.CommitLSN
    onApplied(m.CommitLSN)
```

Commits the transaction, updates the last LSN, and invokes the callback.

## DML Generation

### INSERT

```go
func (a *Applier) applyInsert(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error
```

Generates:
```sql
INSERT INTO "schema"."table" ("col1", "col2", ...) VALUES ($1, $2, ...)
```

- Builds column list, values, and placeholders from `m.NewTuple`
- All identifiers are quoted using `quoteIdent()` to handle reserved words and special characters
- Values are passed as parameterized arguments ($1, $2, ...) to prevent SQL injection
- Skips if `NewTuple` is nil

### UPDATE

```go
func (a *Applier) applyUpdate(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error
```

Generates:
```sql
UPDATE "schema"."table" SET "col1" = $1, "col2" = $2 WHERE "pk1" = $3 AND "pk2" = $4
```

- SET clause built from `m.NewTuple` columns
- WHERE clause built from `m.OldTuple` (preferred, contains replica identity columns) or `m.NewTuple` (fallback)
- Placeholder numbering continues from SET clause into WHERE clause (`$N+1`, `$N+2`, ...)
- Skips if `NewTuple` is nil

### DELETE

```go
func (a *Applier) applyDelete(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error
```

Generates:
```sql
DELETE FROM "schema"."table" WHERE "pk1" = $1 AND "pk2" = $2
```

- WHERE clause built from `m.OldTuple` (preferred) or `m.NewTuple` (fallback)
- Uses the cached `RelationMessage` for column metadata

## Helper Functions

### `buildInsertParts(tuple *TupleData) (cols, vals, placeholders)`

Iterates over tuple columns, producing:
- `cols`: quoted column names
- `vals`: column values as `[]any` (cast from `[]byte` to `string`)
- `placeholders`: `$1`, `$2`, etc.

### `buildSetClauses(tuple *TupleData) (clauses, vals)`

Produces SET clause fragments: `"col" = $N` for each column.

### `buildWhereClauses(m, rel, offset) (clauses, vals)`

Produces WHERE clause fragments using the old tuple (replica identity) or new tuple as fallback. The `offset` parameter ensures placeholder numbering doesn't conflict with SET clauses.

### `qualifiedName(namespace, table) string`

Returns `"schema"."table"` or just `"table"` if namespace is empty or `public`.

### `quoteIdent(s) string`

Quotes a SQL identifier: wraps in double quotes and escapes internal double quotes. This prevents SQL injection and handles reserved words:
```
users      → "users"
order      → "order"       (reserved word)
my"table   → "my""table"   (escaped quote)
```

## LSN Tracking

```go
func (a *Applier) LastLSN() pglogrepl.LSN
```

Returns the LSN of the most recently committed transaction. Thread-safe (protected by mutex). Used by the switchover coordinator to determine the current replication position.

## Error Handling

The applier follows a fail-fast strategy:
- Any DML error immediately rolls back the current transaction and returns the error
- The pipeline's run method receives the error and can decide to retry or abort
- Transaction failures are logged with full context: operation type, namespace, table name

## Transaction Boundaries

The applier strictly preserves source transaction boundaries:
- Each source transaction maps to exactly one destination transaction
- `BEGIN` → process all changes → `COMMIT` (or `ROLLBACK` on error)
- This ensures atomicity: either all changes in a source transaction are applied, or none are
- The destination sees the same transactional grouping as the source

## Value Handling

Column values from the WAL stream are received as `[]byte` and passed to pgx as `string` values. pgx handles the type conversion based on the destination column types. This approach:
- Avoids type mapping complexity in the applier
- Lets PostgreSQL handle type coercion
- Works correctly for all standard PostgreSQL types

## Close

```go
func (a *Applier) Close()
```

No-op — the connection pool is managed externally by the pipeline. The applier holds no resources that need explicit cleanup.
