# LSN Utilities

**Package:** `pkg/lsn`
**File:** `lsn.go`

## Overview

The lsn package provides public utility functions for working with PostgreSQL Log Sequence Numbers (LSNs). LSNs are the fundamental unit of replication progress — they identify specific positions in the Write-Ahead Log. This package calculates replication lag (the byte distance between two LSN positions) and formats it into human-readable strings for display in the TUI, Web UI, and CLI status output.

This package lives under `pkg/` rather than `internal/` because it contains general-purpose LSN utilities that could be useful to external consumers of the pgmanager module.

## What is an LSN?

A Log Sequence Number (LSN) is a 64-bit unsigned integer that represents a byte offset into the PostgreSQL WAL stream. It's displayed in the format `X/YYYYYYYY` where X is the upper 32 bits and Y is the lower 32 bits (both in hexadecimal).

Examples:
- `0/0` — The beginning of the WAL
- `0/1A3B4C5` — Position ~27 MB into the WAL
- `1/0` — Position 4 GB into the WAL (upper 32 bits incremented)

LSNs only move forward. A higher LSN means a later position in the WAL. The difference between two LSNs gives the byte distance between them.

## Functions

### `Lag(current, latest pglogrepl.LSN) uint64`

Calculates the byte distance between two LSN positions:

```go
func Lag(current, latest pglogrepl.LSN) uint64 {
    if latest <= current {
        return 0
    }
    return uint64(latest - current)
}
```

| Parameter | Description |
|-----------|-------------|
| `current` | The LSN that has been applied/confirmed (the consumer's position) |
| `latest` | The most recent LSN received from the source (the producer's position) |

**Returns:** The number of bytes between `current` and `latest`. Returns `0` if `current` is ahead of or equal to `latest` (no lag).

**Safety:** The `latest <= current` guard prevents underflow. This can happen briefly during startup when the confirmed LSN hasn't been initialized yet, or during switchover when the consumer catches up exactly to the producer.

**Usage in the pipeline:**

```go
import "github.com/jfoltran/pgmanager/pkg/lsn"

lagBytes := lsn.Lag(applier.LastLSN(), decoder.LatestLSN())
```

The metrics collector calls this periodically to track replication lag and stores the result in `Snapshot.LagBytes`.

### `FormatLag(bytes uint64, latency time.Duration) string`

Formats replication lag into a human-readable string combining byte distance and time latency:

```go
func FormatLag(bytes uint64, latency time.Duration) string
```

| Parameter | Description |
|-----------|-------------|
| `bytes` | Byte distance from `Lag()` |
| `latency` | Time-based latency estimate (e.g., time since last applied commit) |

**Returns:** A formatted string like `"1.25 MB (latency: 150ms)"`

**Size formatting tiers:**

| Threshold | Format | Example |
|-----------|--------|---------|
| >= 1 GB (2^30) | `%.2f GB` | `1.50 GB` |
| >= 1 MB (2^20) | `%.2f MB` | `12.34 MB` |
| >= 1 KB (2^10) | `%.2f KB` | `256.00 KB` |
| < 1 KB | `%d B` | `512 B` |

Uses binary prefixes (1 KB = 1024 bytes, 1 MB = 1048576 bytes, 1 GB = 1073741824 bytes), which matches PostgreSQL's internal conventions for WAL sizes.

**Latency truncation:** The `time.Duration` is truncated to millisecond precision via `latency.Truncate(time.Millisecond)`. This removes noisy sub-millisecond digits that would clutter the display.

**Output examples:**

```
0 B (latency: 0s)                  — Fully caught up
1.25 KB (latency: 5ms)             — Minimal lag
12.50 MB (latency: 150ms)          — Moderate lag
1.00 GB (latency: 30s)             — Significant lag (initial sync)
```

## Usage Across Components

### Metrics Collector

The collector calls `Lag()` when building snapshots and stores the formatted result:

```go
lagBytes := lsn.Lag(c.appliedLSN, c.latestLSN)
snap.LagBytes = lagBytes
snap.LagFormatted = lsn.FormatLag(lagBytes, latency)
```

### TUI Lag Component

The TUI's lag component (`internal/tui/components/lag.go`) uses `Snapshot.LagBytes` for the sparkline visualization and `Snapshot.LagFormatted` for the text display.

### Web UI

The frontend receives `lag_bytes` and `lag_formatted` in the WebSocket snapshot and displays them in the metrics cards and lag chart.

### CLI Status

The `pgmanager status` command reads `LagFormatted` from the state file:

```
Lag:          12.50 MB (latency: 150ms)
```

## Design Decisions

### Why Not Use `pg_stat_replication`?

PostgreSQL provides `pg_stat_replication.sent_lsn` and `replay_lsn` for lag monitoring. However, pgmanager calculates lag internally because:

1. **No extra queries** — Avoids polling `pg_stat_replication` on the source, which would add load
2. **More accurate** — The internal calculation uses the exact LSN positions maintained by the decoder and applier
3. **Works offline** — The state file preserves lag data even when the pipeline isn't connected
4. **Consistent with architecture** — pgmanager's zero-footprint design avoids unnecessary interactions with the source database

### Why Public (`pkg/`) Instead of Internal?

The LSN utility functions are:
- Stateless and side-effect free
- Useful independently of the rest of pgmanager
- Stable in their interface (unlikely to change)

This makes them good candidates for a public package that could be imported by external tools or tests.
