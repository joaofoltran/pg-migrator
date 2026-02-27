# Metrics Collector

**Package:** `internal/metrics`
**Files:** `collector.go`, `state.go`

## Overview

The metrics collector is the central observability layer for pgmigrator. It aggregates real-time data from the pipeline — phase transitions, per-table copy progress, LSN positions, throughput rates, errors, and log entries — into a unified `Snapshot` struct. Both the HTTP API (including WebSocket) and the TUI consume this data through the same interface, ensuring a single source of truth for all monitoring surfaces.

## Architecture

```
Pipeline ──► Collector ──► Snapshot()  ──► HTTP handlers (REST)
                │                          WebSocket hub (push)
                │                          TUI model (in-process)
                │
                └──► StatePersister ──► ~/.pgmigrator/state.json
                                        └──► `pgmigrator status` (offline)
```

The collector is designed to be fully thread-safe. The pipeline writes to it from multiple goroutines (COPY workers, applier callback, phase transitions), while the HTTP server, WebSocket hub, and TUI read from it concurrently. All access is protected by a combination of `sync.RWMutex`, `sync.Mutex`, and `sync/atomic` operations chosen to minimize lock contention on hot paths.

## Core Types

### `TableStatus`

An enum representing the lifecycle state of a single table during migration:

| Value       | Description                                      |
|-------------|--------------------------------------------------|
| `pending`   | Table is queued for copy but not yet started      |
| `copying`   | Table is actively being copied via COPY protocol  |
| `copied`    | Initial COPY is complete                          |
| `streaming` | Table is receiving live CDC changes               |

### `TableProgress`

Tracks per-table migration progress. Each field is JSON-serializable for the API:

| Field        | Type          | Description                                          |
|--------------|---------------|------------------------------------------------------|
| `Schema`     | `string`      | PostgreSQL schema name (e.g., `public`)              |
| `Name`       | `string`      | Table name                                           |
| `Status`     | `TableStatus` | Current lifecycle state                              |
| `RowsTotal`  | `int64`       | Estimated total rows from `pg_stat_user_tables`      |
| `RowsCopied` | `int64`       | Number of rows copied so far                         |
| `SizeBytes`  | `int64`       | Table size from `pg_table_size()`                    |
| `BytesCopied`| `int64`       | Bytes written to destination                         |
| `Percent`    | `float64`     | Completion percentage (0-100)                        |
| `ElapsedSec` | `float64`     | Seconds since copy started for this table            |
| `StartedAt`  | `time.Time`   | Copy start timestamp (excluded from JSON via `json:"-"`) |

### `Snapshot`

The complete metrics state at a point in time. This is the primary data structure consumed by all UI surfaces:

| Field          | Type              | Description                                        |
|----------------|-------------------|----------------------------------------------------|
| `Timestamp`    | `time.Time`       | When this snapshot was taken                       |
| `Phase`        | `string`          | Current pipeline phase (idle, connecting, schema, copy, streaming, switchover, done) |
| `ElapsedSec`   | `float64`         | Total seconds since pipeline started               |
| `AppliedLSN`   | `string`          | Last LSN applied to destination                    |
| `ConfirmedLSN` | `string`          | Last LSN confirmed (flushed) back to source        |
| `LagBytes`     | `uint64`          | Byte distance between applied and latest LSN       |
| `LagFormatted` | `string`          | Human-readable lag (e.g., "1.23 MB (latency: 0s)") |
| `TablesTotal`  | `int`             | Total number of tables in migration                |
| `TablesCopied` | `int`             | Tables with status `copied` or `streaming`         |
| `Tables`       | `[]TableProgress` | Per-table detail list                              |
| `RowsPerSec`   | `float64`         | Current throughput: rows per second (60s window)   |
| `BytesPerSec`  | `float64`         | Current throughput: bytes per second (60s window)  |
| `TotalRows`    | `int64`           | Cumulative rows processed                          |
| `TotalBytes`   | `int64`           | Cumulative bytes processed                         |
| `ErrorCount`   | `int`             | Total error count                                  |
| `LastError`    | `string`          | Most recent error message (omitted if empty)       |

### `LogEntry`

Captured log lines for the UI log viewer:

| Field    | Type                    | Description                           |
|----------|------------------------|---------------------------------------|
| `Time`   | `time.Time`            | Log timestamp                         |
| `Level`  | `string`               | Log level: debug, info, warn, error   |
| `Message`| `string`               | Log message text                      |
| `Fields` | `map[string]string`    | Optional structured fields            |

## Collector

### Construction

```go
collector := metrics.NewCollector(logger)
defer collector.Close()
```

`NewCollector` initializes all internal data structures:
- Table tracking map and insertion-order list
- Subscriber registry for push-based updates
- Two sliding windows (rows and bytes) with 60-second windows
- Log ring buffer with 500-entry capacity
- Starts the background `broadcastLoop` goroutine

### Pipeline Integration Methods

These methods are called by the pipeline during migration:

**`SetPhase(phase string)`** — Called on every phase transition. Also records `startedAt` timestamp on first call.

**`SetTables(tables []TableProgress)`** — Initializes per-table tracking. Called once after `ListTables()` returns the table list. Preserves insertion order for consistent display.

**`TableStarted(schema, name string)`** — Marks a table as `copying` and records the start time. Called when a COPY worker picks up a table.

**`UpdateTableProgress(schema, name string, rowsCopied, bytesCopied int64)`** — Updates in-flight progress for a table. Automatically recalculates `Percent` and `ElapsedSec`.

**`TableDone(schema, name string, rowsCopied int64)`** — Marks a table as `copied` with 100% completion.

**`TableStreaming(schema, name string)`** — Marks a table as `streaming` (post-COPY CDC phase).

**`RecordApplied(appliedLSN pglogrepl.LSN, rows int64, bytes int64)`** — Records a successful write to the destination. Updates the applied LSN, increments cumulative counters (atomically), and feeds the sliding windows for throughput calculation.

**`RecordConfirmedLSN(lsn pglogrepl.LSN)`** — Updates the confirmed/flushed LSN position.

**`RecordLatestLSN(lsn pglogrepl.LSN)`** — Updates the server-reported write position for lag calculation.

**`RecordError(err error)`** — Atomically increments the error counter and stores the error message.

**`AddLog(entry LogEntry)`** — Appends to the ring buffer. When the buffer reaches capacity (500), the oldest 25% of entries are evicted in bulk to amortize the copy cost.

### Read Methods

**`Snapshot() Snapshot`** — Returns a complete, consistent point-in-time snapshot. Acquires a read lock (`RLock`) to minimize contention. Computes derived values (elapsed time, lag bytes, throughput rates) at read time.

**`Logs() []LogEntry`** — Returns a copy of the log ring buffer contents.

### Pub/Sub

**`Subscribe() chan Snapshot`** — Returns a buffered channel (capacity 4) that receives periodic snapshots. The collector's background goroutine pushes to all subscribers every 500ms.

**`Unsubscribe(ch chan Snapshot)`** — Removes a subscriber. Must be called to prevent goroutine leaks.

**`Close()`** — Stops the broadcast loop. Safe to call multiple times.

### Broadcast Loop

A background goroutine started by `NewCollector`:
- Ticks every 500ms
- Takes a snapshot
- Non-blocking send to all subscriber channels (slow consumers are skipped, not blocked)
- Stops when `Close()` is called

### Sliding Window Throughput

The `slidingWindow` type maintains a time-ordered list of `(timestamp, value)` entries within a configurable window (60 seconds for both rows and bytes).

**`Add(t time.Time, val float64)`** — Appends an entry and evicts expired entries.

**`Rate() float64`** — Computes `sum(values) / elapsed_seconds`. Returns 0 if the window is empty. Minimum elapsed time is clamped to 1 second to avoid division spikes.

**`evict(now time.Time)`** — Removes entries older than `now - window` using an in-place slice shift to avoid allocations.

### Concurrency Model

| Data | Protection | Rationale |
|------|-----------|-----------|
| Phase, tables, LSN positions | `sync.RWMutex` | Frequent reads from Snapshot(), infrequent writes |
| Total rows/bytes | `atomic.Int64` | Hot path in RecordApplied, no lock needed |
| Error count | `atomic.Int64` | Same as above |
| Last error | `atomic.Value` | Lock-free string storage |
| Sliding windows | Per-window `sync.Mutex` | Independent of main lock |
| Subscribers | `sync.Mutex` | Separate from data lock |
| Logs | `sync.Mutex` | Separate ring buffer lock |

## State Persister

### Purpose

The `StatePersister` bridges the gap between a running pipeline and the `pgmigrator status` command. It periodically serializes the current `Snapshot` to `~/.pgmigrator/state.json` so that status can be checked even when the pipeline is not running (e.g., after a crash or normal completion).

### Construction

```go
persister, err := metrics.NewStatePersister(collector, logger)
persister.Start()
defer persister.Stop()
```

Creates the `~/.pgmigrator/` directory if it doesn't exist (mode 0755).

### Write Strategy

- Writes every 2 seconds via a ticker-driven goroutine
- Uses atomic file writes: writes to `state.json.tmp` first, then renames to `state.json`
- This prevents readers from seeing a partially-written file
- On `Stop()`, performs one final write to capture the terminal state

### State File Format

Pretty-printed JSON (indented with 2 spaces) matching the `Snapshot` struct:

```json
{
  "timestamp": "2026-02-27T14:23:01.234Z",
  "phase": "streaming",
  "elapsed_sec": 5025.7,
  "applied_lsn": "0/1A3B4C5",
  "confirmed_lsn": "0/1A3B4C0",
  "lag_bytes": 1258291,
  "lag_formatted": "1.20 MB (latency: 0s)",
  "tables_total": 54,
  "tables_copied": 42,
  "tables": [...],
  "rows_per_sec": 4521.3,
  "bytes_per_sec": 1048576.0,
  "total_rows": 22670800,
  "total_bytes": 5368709120,
  "error_count": 0
}
```

### Reading State

```go
snap, err := metrics.ReadStateFile()
```

Reads and deserializes `~/.pgmigrator/state.json`. Returns an error if the file doesn't exist (which the `status` command handles gracefully with a user-friendly message). The `status` command also checks the `Timestamp` field to detect staleness — if the state is older than 10 seconds, it warns the user.

### File Location

| Platform | Path |
|----------|------|
| Linux    | `~/.pgmigrator/state.json` |
| macOS    | `~/.pgmigrator/state.json` |

The directory and file are created with standard permissions (0755 for directory, 0644 for file).
