# Snapshot (Parallel COPY)

**Package:** `internal/migration/snapshot`
**File:** `snapshot.go`

## Overview

The snapshot package implements the initial data load phase of migrator. It copies all tables from the source database to the destination using PostgreSQL's COPY protocol with parallel workers and consistent snapshots. This is the performance-critical path — for large databases, the COPY phase dominates total migration time.

## Architecture

```
                        Copier.CopyAll()
                             │
                   ┌─────────┼─────────┐
                   ▼         ▼         ▼
              Worker 0   Worker 1   Worker 2   ... Worker N
                   │         │         │
         SET TRANSACTION SNAPSHOT 'name'
                   │         │         │
         SELECT * FROM table_a  table_b  table_c
                   │         │         │
                   ▼         ▼         ▼
              CopyFrom   CopyFrom   CopyFrom
                   │         │         │
                   └─────────┼─────────┘
                             ▼
                      Destination PG
```

## Types

### `TableInfo`

Describes a table eligible for COPY:

```go
type TableInfo struct {
    Schema    string  // PostgreSQL schema name (e.g., "public")
    Name      string  // Table name
    RowCount  int64   // Estimated row count from pg_stat_user_tables
    SizeBytes int64   // Table size from pg_table_size()
}
```

**`QualifiedName() string`** — Returns `schema.table` or just `table` for the public schema. Used in log messages and error reporting.

### `CopyResult`

Holds the outcome of copying a single table:

```go
type CopyResult struct {
    Table      TableInfo  // The table that was copied
    RowsCopied int64      // Actual rows written to destination
    Err        error      // Non-nil if copy failed
}
```

### `Copier`

The parallel COPY engine:

```go
type Copier struct {
    source  *pgxpool.Pool   // Source connection pool
    dest    *pgxpool.Pool   // Destination connection pool
    logger  zerolog.Logger  // Component-tagged logger
    workers int              // Number of parallel COPY workers
}
```

## Construction

```go
copier := snapshot.NewCopier(srcPool, dstPool, workers, logger)
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `srcPool` | — | Source PostgreSQL connection pool |
| `dstPool` | — | Destination PostgreSQL connection pool |
| `workers` | 4 | Number of parallel COPY goroutines |
| `logger` | — | zerolog logger, tagged as `component: snapshot` |

## Table Discovery

```go
func (c *Copier) ListTables(ctx context.Context) ([]TableInfo, error)
```

Queries `pg_stat_user_tables` to discover all user tables:

```sql
SELECT schemaname, tablename,
    COALESCE(n_live_tup, 0),
    COALESCE(pg_table_size(schemaname || '.' || tablename), 0)
FROM pg_stat_user_tables
ORDER BY pg_table_size(schemaname || '.' || tablename) DESC
```

**Key design decisions:**
- **Largest tables first:** Sorted by `pg_table_size()` descending so that the biggest tables start copying first, optimizing overall parallelism
- **Row count estimate:** Uses `n_live_tup` from pg_stat which is an estimate (updated by ANALYZE/autovacuum), not an exact count. Good enough for progress reporting
- **Size estimate:** Uses `pg_table_size()` which includes TOAST data and free space map, giving a realistic byte count
- **User tables only:** `pg_stat_user_tables` automatically excludes system catalogs

## Parallel COPY

```go
func (c *Copier) CopyAll(ctx context.Context, tables []TableInfo, snapshotName string) []CopyResult
```

Orchestrates parallel table copying:

1. Creates a buffered work channel and enqueues all tables
2. Launches `c.workers` goroutines, each pulling tables from the work channel
3. Each worker calls `copyTable()` for each table
4. Results are collected in a mutex-protected slice
5. Waits for all workers to complete

### Worker Distribution

```
Work Channel: [table_a, table_b, table_c, table_d, table_e]
                  │          │          │
Worker 0 ◄───────┘          │          │
Worker 1 ◄──────────────────┘          │
Worker 2 ◄─────────────────────────────┘
Worker 0 ◄─── (takes table_d when done with table_a)
Worker 1 ◄─── (takes table_e when done with table_b)
```

Because the largest tables are enqueued first, workers naturally balance: fast-to-copy small tables are picked up as workers finish large ones.

## Single Table COPY (`copyTable`)

```go
func (c *Copier) copyTable(ctx context.Context, table TableInfo, snapshotName string, workerID int) CopyResult
```

Copies a single table from source to destination:

### Step 1: Acquire Source Connection and Set Snapshot

```go
srcConn, _ := c.source.Acquire(ctx)
srcTx, _ := srcConn.BeginTx(ctx, pgx.TxOptions{
    IsoLevel:   pgx.RepeatableRead,
    AccessMode: pgx.ReadOnly,
})
srcTx.Exec(ctx, fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", snapshotName))
```

- Acquires a dedicated connection from the source pool
- Opens a read-only, repeatable-read transaction
- Sets the transaction's snapshot to the one captured when the replication slot was created
- This ensures all workers see the same consistent point-in-time view

### Step 2: Read All Rows

```go
rows, _ := srcTx.Query(ctx, fmt.Sprintf("SELECT * FROM %s", qn))
fieldDescs := rows.FieldDescriptions()
var copyRows [][]any
for rows.Next() {
    vals, _ := rows.Values()
    copyRows = append(copyRows, vals)
}
```

- Queries all rows from the table
- Extracts column names from field descriptions
- Collects all rows into memory as `[][]any` slices

### Step 3: Write to Destination via COPY

```go
count, _ := c.dest.CopyFrom(ctx,
    pgx.Identifier{table.Schema, table.Name},
    colNames,
    pgx.CopyFromRows(copyRows))
```

- Uses pgx's `CopyFrom` which internally uses the PostgreSQL COPY protocol
- COPY is significantly faster than individual INSERT statements
- The `pgx.Identifier` properly quotes schema and table names
- Returns the actual number of rows written

### Step 4: Return Result

```go
return CopyResult{Table: table, RowsCopied: count}
```

## Snapshot Consistency

The snapshot mechanism ensures that the initial COPY captures a consistent point-in-time view of the source database, even though tables are copied in parallel over potentially minutes or hours.

### How It Works

1. The decoder creates a replication slot:
   ```go
   result, _ := pglogrepl.CreateReplicationSlot(ctx, conn, slotName, "pgoutput", ...)
   snapshotName = result.SnapshotName
   ```

2. The snapshot name is passed to the copier:
   ```go
   results := copier.CopyAll(ctx, tables, snapshotName)
   ```

3. Each COPY worker sets its transaction to use this snapshot:
   ```sql
   SET TRANSACTION SNAPSHOT '00000003-0000001A-1'
   ```

4. All workers see the same database state, regardless of concurrent writes

### Gap-Free, Duplicate-Free Guarantee

Because the replication slot's consistent point matches the snapshot:
- The COPY captures all data up to the consistent point
- The WAL stream starts from the consistent point
- There is no gap (data committed between COPY and WAL start)
- There is no overlap (data included in both COPY and WAL)

## Error Handling

- Each `copyTable` failure is captured in the `CopyResult.Err` field
- The pipeline checks each result and aborts on the first error
- Source transactions are rolled back via `defer srcTx.Rollback(ctx)`
- Source connections are released via `defer srcConn.Release()`

## Performance Considerations

- **COPY protocol**: 10-100x faster than individual INSERTs for bulk data
- **Parallel workers**: Saturates network and disk I/O across multiple tables
- **Largest-first ordering**: Prevents long tail where one huge table delays completion
- **In-memory buffering**: All rows are loaded into memory before writing. For very large tables (>RAM), this is a known limitation that could be addressed with streaming COPY

## Logging

Each table copy is logged at INFO level:
- `"starting COPY"` — with table name and worker ID
- `"COPY complete"` — with table name and row count
