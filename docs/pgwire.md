# PG Wire (Protocol Helpers)

**Package:** `internal/migration/pgwire`
**File:** `pgwire.go`

## Overview

The pgwire package provides a thin wrapper around `pgconn.PgConn` with replication-specific helper methods. It abstracts the low-level PostgreSQL protocol operations needed for bidirectional loop detection (replication origin management) and replication slot cleanup. The name "pgwire" refers to PostgreSQL's wire protocol — the binary protocol used for client-server communication.

## Architecture

```
                    pgwire.Conn
                        │
                        ├── SetReplicationOrigin()
                        │     │
                        │     ├── pg_replication_origin_create()
                        │     └── pg_replication_origin_session_setup()
                        │
                        ├── DropReplicationSlot()
                        │     └── pg_drop_replication_slot()
                        │
                        ├── Raw() → *pgconn.PgConn
                        │
                        └── Close()
```

## Why a Wrapper?

The `pgconn.PgConn` is the low-level PostgreSQL connection from the pgx library. It exposes the raw wire protocol, which is powerful but verbose. The `Conn` wrapper provides:

1. **Consistent logging** — All operations are tagged with `component: pgwire`
2. **Error wrapping** — Raw protocol errors are wrapped with descriptive context
3. **Convenience** — Multi-step operations (like origin creation + session setup) are combined into single method calls
4. **`exec` helper** — Properly drains the `MultiResultReader` to avoid connection state corruption

## Types

### `Conn`

```go
type Conn struct {
    conn   *pgconn.PgConn   // Underlying low-level PostgreSQL connection
    logger zerolog.Logger    // Component-tagged logger
}
```

The wrapper holds a direct reference to `pgconn.PgConn` (not a pool connection). This is important because replication operations require a dedicated connection that stays in a specific protocol state.

## Construction

```go
conn := pgwire.NewConn(rawConn, logger)
```

Creates a `Conn` wrapper around an existing `pgconn.PgConn`. The logger is tagged with `component: pgwire` for filtering in structured log output.

| Parameter | Type | Description |
|-----------|------|-------------|
| `rawConn` | `*pgconn.PgConn` | Low-level PostgreSQL connection |
| `logger` | `zerolog.Logger` | Parent logger (will be sub-tagged) |

## Methods

### `Raw() *pgconn.PgConn`

Returns the underlying `pgconn.PgConn` for operations that need direct protocol access. Used by the `stream.Decoder` which calls `pglogrepl.StartReplication()` and `pglogrepl.SendStandbyStatusUpdate()` directly on the raw connection.

### `SetReplicationOrigin(ctx, originName) error`

Configures a replication origin on the connection for bidirectional loop detection. This is a two-step operation:

**Step 1: Create the origin (idempotent)**

```sql
SELECT pg_replication_origin_create('migrator-a')
WHERE NOT EXISTS (
    SELECT 1 FROM pg_replication_origin WHERE roname = 'migrator-a'
)
```

The `WHERE NOT EXISTS` clause makes the creation idempotent — calling `SetReplicationOrigin` multiple times with the same name is safe. Without this guard, `pg_replication_origin_create()` would raise an error if the origin already exists.

**Step 2: Attach the origin to the current session**

```sql
SELECT pg_replication_origin_session_setup('migrator-a')
```

After this call, all writes made through this connection are tagged with the specified origin. When these writes appear in the WAL stream, they carry an `OriginMessage` with the origin name, which the `bidi.Filter` uses to detect and drop looped messages.

**Error wrapping:**
- Step 1 failure: `"create replication origin: <underlying error>"`
- Step 2 failure: `"setup replication origin session: <underlying error>"`

**Logging:** On success, logs at INFO level: `replication origin configured` with the origin name.

### `DropReplicationSlot(ctx, slotName) error`

Drops a replication slot from the connected PostgreSQL instance:

```sql
SELECT pg_drop_replication_slot('migrator')
```

This is used during cleanup — removing the slot when the migration is complete or when starting fresh. Dropping a replication slot releases the WAL segments it was holding, freeing disk space on the source server.

**Important:** This will fail if the slot is currently active (being consumed by another connection). The caller must ensure the streaming connection is closed before dropping the slot.

**Error wrapping:** `"drop replication slot: <underlying error>"`

### `Close(ctx) error`

Closes the underlying `pgconn.PgConn` connection. This is a clean protocol shutdown — it sends a Terminate message to PostgreSQL before closing the TCP connection.

## Internal Helper

### `exec(ctx, sql) ([]byte, error)`

A helper method that executes a SQL statement through the low-level `pgconn` protocol and properly drains all results:

```go
func (c *Conn) exec(ctx context.Context, sql string) ([]byte, error) {
    mrr := c.conn.Exec(ctx, sql)
    var result []byte
    for mrr.NextResult() {
        buf := mrr.ResultReader().Read()
        if buf.Err != nil {
            return nil, buf.Err
        }
    }
    return result, mrr.Close()
}
```

**Why this pattern matters:**

The `pgconn.PgConn.Exec()` method returns a `MultiResultReader` because PostgreSQL's simple query protocol can return multiple result sets from a single query string. The helper must:

1. Call `NextResult()` to advance through each result set
2. Call `ResultReader().Read()` to consume each result (the `Read()` method returns a `Result` struct — access `.Err` for errors, not a `([]byte, error)` tuple)
3. Call `mrr.Close()` to finalize the multi-result reader

Failing to drain all results would leave the connection in an invalid protocol state, causing subsequent operations to fail with confusing errors.

## Usage in the Pipeline

### Replication Origin Setup

In bidirectional mode, the pipeline configures the origin on the destination connection before applying changes:

```go
// In the applier's connection setup:
if cfg.Replication.OriginID != "" {
    pgwireConn := pgwire.NewConn(rawConn, logger)
    err := pgwireConn.SetReplicationOrigin(ctx, cfg.Replication.OriginID)
}
```

### Slot Cleanup

When a migration is complete or cancelled, the pipeline can clean up the replication slot:

```go
pgwireConn := pgwire.NewConn(rawConn, logger)
err := pgwireConn.DropReplicationSlot(ctx, cfg.Replication.SlotName)
```

## PostgreSQL Replication Origin Internals

### What is a Replication Origin?

A replication origin is a PostgreSQL feature (since PG 9.5) that allows tagging writes with a named origin. It's stored in the `pg_replication_origin` catalog:

| Column | Type | Description |
|--------|------|-------------|
| `roident` | `oid` | Internal numeric ID |
| `roname` | `text` | The origin name (e.g., `"migrator-a"`) |

### How Origin Tagging Works

1. A connection calls `pg_replication_origin_session_setup('name')` to attach an origin
2. All subsequent writes on that connection are tagged with the origin's OID in the WAL
3. When the logical decoding plugin (pgoutput) encounters these WAL records, it emits an `OriginMessage` before the corresponding data messages
4. The decoder captures the origin name, and the bidi filter uses it for loop detection

### Session vs. Transaction Origins

`pg_replication_origin_session_setup()` sets the origin for the entire session (connection lifetime), not per-transaction. This is more efficient than per-transaction origin setup and matches migrator's connection model where the applier uses a dedicated pool.

## Thread Safety

The `Conn` wrapper is **not** thread-safe. It wraps a single `pgconn.PgConn` which is a stateful protocol connection. All calls to a `Conn` instance must be serialized by the caller. In migrator, this is naturally enforced because:
- The decoder uses one connection for the replication stream
- The applier uses pool connections (one at a time per transaction)
- Origin setup happens once during initialization, before concurrent operations begin
