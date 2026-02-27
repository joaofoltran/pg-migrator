# Pipeline

**Package:** `internal/pipeline`
**File:** `pipeline.go`

## Overview

The pipeline is the central orchestrator of pgmigrator. It wires together all components — decoder, applier, copier, schema migrator, sentinel coordinator, and bidi filter — and manages the full migration lifecycle through a series of phases. It owns all database connections, coordinates parallel operations, and feeds the metrics collector with real-time progress data.

## Architecture

```
              ┌──────────────────────────────────────────────────────────────────┐
              │                         Pipeline                                  │
              │                                                                   │
              │  ┌─────────┐   ┌──────────┐   ┌──────────┐   ┌──────────────┐   │
              │  │ Decoder  │──►│  Filter  │──►│ Applier  │──►│  Destination │   │
              │  │  (WAL)   │   │  (bidi)  │   │  (DML)   │   │   Database   │   │
              │  └─────────┘   └──────────┘   └──────────┘   └──────────────┘   │
              │       │                                                           │
              │       │  ┌──────────┐   ┌──────────────┐                         │
              │       └──│  Copier  │──►│  Destination  │   (parallel COPY)      │
              │          │(snapshot)│   │   Database    │                         │
              │          └──────────┘   └──────────────┘                         │
              │                                                                   │
              │  ┌───────────┐  ┌─────────────┐  ┌──────────┐                   │
              │  │  Metrics  │  │  Persister   │  │Coordinator│                  │
              │  │ Collector │  │ (state file) │  │(sentinel) │                  │
              │  └───────────┘  └─────────────┘  └──────────┘                   │
              └──────────────────────────────────────────────────────────────────┘
```

## Pipeline Phases

The migration progresses through a series of phases, each tracked in both the `Progress` struct and the `metrics.Collector`:

| Phase | Description | Duration |
|-------|-------------|----------|
| `idle` | Pipeline created, not yet started | Instantaneous |
| `connecting` | Establishing database connections | 1-5 seconds |
| `schema` | Dumping source DDL and applying to destination | Seconds to minutes |
| `copy` | Parallel COPY of all tables via consistent snapshot | Minutes to hours |
| `streaming` | Live CDC replication from WAL stream | Indefinite |
| `switchover` | Sentinel injection and confirmation | Seconds |
| `switchover-complete` | Destination confirmed caught up | Terminal |
| `done` | Clone-only operation completed | Terminal |

## Core Types

### `Progress`

Lightweight in-memory progress tracking (the original struct, retained for backward compatibility):

```go
type Progress struct {
    Phase        string
    LastLSN      pglogrepl.LSN
    TablesTotal  int
    TablesCopied int
    StartedAt    time.Time
}
```

### `Pipeline`

The main struct holding all state:

```go
type Pipeline struct {
    cfg    *config.Config        // Full configuration
    logger zerolog.Logger        // Component-tagged logger

    // Connections
    replConn *pgconn.PgConn      // Replication protocol connection to source
    srcPool  *pgxpool.Pool       // Source connection pool (for COPY workers)
    dstPool  *pgxpool.Pool       // Destination connection pool

    // Components
    decoder     *stream.Decoder       // WAL stream consumer
    applier     *replay.Applier       // DML writer to destination
    copier      *snapshot.Copier      // Parallel COPY engine
    schemaMgr   *schema.Migrator      // DDL dump/apply/compare
    coordinator *sentinel.Coordinator // Switchover sentinel manager
    bidiFilter  *bidi.Filter          // Loop detection (optional)

    // Metrics
    Metrics   *metrics.Collector      // Public: consumed by server/TUI
    persister *metrics.StatePersister  // State file writer

    // Internal
    messages chan stream.Message   // Pipeline message channel (256 buffer)
    mu       sync.Mutex           // Protects progress
    progress Progress             // In-memory progress
    cancel   context.CancelFunc   // Cancels all pipeline operations
}
```

## Lifecycle Methods

### `New(cfg, logger) *Pipeline`

Creates a new pipeline with:
- A configured `metrics.Collector`
- A 256-buffered message channel
- Phase set to `idle`
- All component references nil (initialized later)

### `connect(ctx) error`

Establishes three database connections:

1. **Replication connection** (`pgconn.Connect`) — Low-level pgconn for the replication protocol (not pgx pool, because replication uses a special protocol mode)
2. **Source pool** (`pgxpool.New`) — For snapshot COPY workers (multiple concurrent connections)
3. **Destination pool** (`pgxpool.New`) — For DML application and COPY writes

All connection strings are built from `config.DatabaseConfig.DSN()` and `ReplicationDSN()`.

### `initComponents()`

Creates all pipeline components using the established connections:
- `stream.Decoder` — Configured with slot name and publication from config
- `replay.Applier` — Uses destination pool
- `snapshot.Copier` — Uses both pools with configured worker count
- `schema.Migrator` — Uses both pools
- `sentinel.Coordinator` — Writes sentinels to the messages channel
- `bidi.Filter` — Only created if `OriginID` is configured

### `startPersister()`

Initializes the `StatePersister` to write `~/.pgmigrator/state.json` every 2 seconds. Logs a warning and continues if state persistence fails (e.g., filesystem permission issues).

## Run Methods

### `RunClone(ctx) error`

Full schema + data copy without CDC streaming:

1. `connecting` → Establish connections
2. `schema` → `pg_dump --schema-only` + apply DDL
3. Create replication slot (for consistent snapshot), drain WAL messages
4. `copy` → List tables, initialize metrics, parallel COPY all tables
5. Track per-table completion in metrics
6. `done` → Log completion

### `RunCloneAndFollow(ctx) error`

Clone then transition to live streaming:

1. Same as `RunClone` through step 4
2. During COPY: buffer incoming WAL messages in a 4096-capacity channel
3. `streaming` → Mark all tables as `streaming` in metrics
4. Wire the buffered channel through optional bidi filter to applier
5. Applier callback: confirm LSN, update metrics
6. Runs until context cancellation

The 4096 buffer prevents message loss during the COPY phase when the applier isn't yet consuming. After COPY completes, the applier drains the buffer and then processes live messages.

### `RunFollow(ctx, startLSN) error`

CDC streaming from a given LSN (slot must already exist):

1. `connecting` → Establish connections
2. Start decoder from `startLSN`
3. `streaming` → Wire through optional bidi filter to applier
4. Applier callback: confirm LSN, update metrics
5. Runs until context cancellation

### `RunSwitchover(ctx, timeout) error`

Zero-downtime switchover via sentinel:

1. `switchover` → Get current applied LSN
2. Inject sentinel message into pipeline
3. Wait for applier to confirm sentinel (or timeout)
4. `switchover-complete` → Log success

### Metrics Integration in Run Methods

Every run method integrates with the metrics collector:

```go
// Phase transitions
p.setPhase("copy")  // Updates both Progress and Collector

// Table tracking
p.initTableMetrics(tables)  // Converts []snapshot.TableInfo → []metrics.TableProgress
p.Metrics.TableDone(r.Table.Schema, r.Table.Name, r.RowsCopied)

// Throughput tracking
p.Metrics.RecordApplied(lsn, rows, bytes)

// Confirmed LSN
p.Metrics.RecordConfirmedLSN(lsn)

// Error tracking
p.Metrics.RecordError(r.Err)
```

## Helper Methods

### `Status() Progress`

Returns a thread-safe copy of the in-memory `Progress` struct. This is the original lightweight status mechanism, now supplemented by the richer `Metrics.Snapshot()`.

### `Config() *config.Config`

Returns the pipeline's configuration reference. Used by the HTTP server's `/api/v1/config` handler.

### `Close()`

Shuts down all components in order:

1. Cancel context (signals all goroutines)
2. Close metrics collector (stops broadcast loop)
3. Stop state persister (writes final state)
4. Close decoder (waits for receive loop to exit)
5. Close applier
6. Close replication connection
7. Close source pool
8. Close destination pool

### `setPhase(phase string)`

Updates both the internal `Progress` and the `metrics.Collector`. Records `startedAt` on the first phase transition. Logs the transition.

### `initTableMetrics(tables []snapshot.TableInfo)`

Converts the copier's `[]snapshot.TableInfo` (with `Schema`, `Name`, `RowCount`, `SizeBytes`) into `[]metrics.TableProgress` (with `Status: TablePending`) and passes to the collector.

## Connection Architecture

```
Source PostgreSQL
  ├── Replication Connection (pgconn, 1 conn)
  │     └── WAL streaming protocol
  └── Connection Pool (pgxpool, N conns)
        └── COPY workers use SET TRANSACTION SNAPSHOT

Destination PostgreSQL
  └── Connection Pool (pgxpool, M conns)
        ├── COPY writes
        └── DML from applier
```

The replication connection uses the raw `pgconn` interface (not `pgx`) because logical replication uses a special protocol flow (CopyData messages) that isn't supported by the standard query interface.
