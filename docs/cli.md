# CLI Commands

**Package:** `cmd/pgmigrator`
**Files:** `main.go`, `root.go`, `clone.go`, `follow.go`, `switchover.go`, `status.go`, `compare.go`, `serve.go`, `tui.go`

## Overview

pgmigrator uses [cobra](https://github.com/spf13/cobra) for its CLI framework. The root command defines global persistent flags (database connections, replication settings, logging), and each subcommand implements a specific migration operation. All commands share the same `config.Config` struct and `zerolog.Logger`, initialized in the root command's `PersistentPreRunE`.

## Command Tree

```
pgmigrator
├── clone        Copy schema + data, optionally follow with CDC
├── follow       Stream CDC changes from source to destination
├── switchover   Zero-downtime switchover via sentinel marker
├── status       Show migration progress from state file
├── compare      Compare source and destination schemas (stub)
├── serve        Start standalone web UI server
└── tui          Launch terminal dashboard for remote monitoring
```

## Entry Point (`main.go`)

```go
func main() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

Standard cobra entry point. Errors bubble up from subcommand `RunE` functions and are printed to stderr.

## Root Command (`root.go`)

### Description

```
pgmigrator is a middleman between source and destination PostgreSQL databases.
It owns the WAL stream, performs parallel COPY with consistent snapshots,
and supports zero-downtime switchover via sentinel markers.
```

### Configuration

| Setting | Value | Purpose |
|---------|-------|---------|
| `SilenceUsage` | `true` | Don't print usage on errors (noisy) |
| `SilenceErrors` | `true` | Errors are printed by `main()` instead |

### Logger Initialization (`PersistentPreRunE`)

Runs before every subcommand. Configures zerolog based on `--log-format` and `--log-level`:

| Format | Behavior |
|--------|----------|
| `"json"` | `zerolog.New(os.Stdout)` — JSON lines to stdout |
| `"console"` (default) | `zerolog.ConsoleWriter{Out: os.Stderr}` — Colored, human-readable to stderr |

Log level is parsed from string. If invalid, defaults to `info`.

### Global Flags

All flags are **persistent** (inherited by all subcommands):

#### Source Database

| Flag | Default | Description |
|------|---------|-------------|
| `--source-host` | `localhost` | Source PostgreSQL host |
| `--source-port` | `5432` | Source PostgreSQL port |
| `--source-user` | `postgres` | Source PostgreSQL user |
| `--source-password` | `""` | Source PostgreSQL password |
| `--source-dbname` | `""` | Source database name (**required**) |

#### Destination Database

| Flag | Default | Description |
|------|---------|-------------|
| `--dest-host` | `localhost` | Destination PostgreSQL host |
| `--dest-port` | `5432` | Destination PostgreSQL port |
| `--dest-user` | `postgres` | Destination PostgreSQL user |
| `--dest-password` | `""` | Destination PostgreSQL password |
| `--dest-dbname` | `""` | Destination database name (**required**) |

#### Replication

| Flag | Default | Description |
|------|---------|-------------|
| `--slot` | `pgmigrator` | Replication slot name |
| `--publication` | `pgmigrator_pub` | Publication name |
| `--output-plugin` | `pgoutput` | Logical decoding output plugin |
| `--origin-id` | `""` | Replication origin ID for bidi loop detection |

#### Snapshot

| Flag | Default | Description |
|------|---------|-------------|
| `--copy-workers` | `4` | Number of parallel COPY workers |

#### Logging

| Flag | Default | Description |
|------|---------|-------------|
| `--log-level` | `info` | Log level (debug, info, warn, error) |
| `--log-format` | `console` | Log format (console, json) |

---

## `clone` — Full Database Copy

### Usage

```bash
pgmigrator clone [flags]
```

### Description

Performs a complete migration of the source database to the destination:

1. **Schema phase** — Dumps DDL from source via `pg_dump`, applies to destination
2. **Snapshot phase** — Creates replication slot (captures snapshot), copies all tables in parallel using consistent snapshot
3. **Follow phase** (optional) — Transitions to CDC streaming, applying WAL changes in real-time

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--follow` | `false` | Continue with CDC streaming after initial copy |
| `--resume` | `false` | Resume an interrupted clone (requires existing replication slot and `--follow`) |
| `--api-port` | `0` | Enable HTTP API/Web UI on this port (0 = disabled) |
| `--tui` | `false` | Show terminal dashboard during migration |

### Behavior

```go
p := pipeline.New(&cfg, logger)
defer p.Close()

if cloneAPIPort > 0 {
    srv := server.New(p.Metrics, &cfg, logger)
    srv.StartBackground(cmd.Context(), cloneAPIPort)
}

run := p.RunClone
if cloneFollow {
    run = p.RunCloneAndFollow
}
if cloneResume {
    run = p.RunResumeCloneAndFollow
}

if cloneTUI {
    // Run pipeline in background goroutine
    // Run TUI in foreground (blocking)
    // Return pipeline error when TUI exits
}

return run(cmd.Context())
```

When `--tui` is enabled, the pipeline runs in a background goroutine while the TUI takes over the terminal. The TUI reads metrics directly from the in-process `Collector`. When the user quits the TUI (press `q`), the pipeline error (if any) is returned.

When `--api-port` is set, the HTTP server starts in a background goroutine, serving the REST API and Web UI on the specified port.

### Resume Mode (`--resume --follow`)

When a clone is interrupted (OOM kill, crash, network failure), `--resume` recovers without data loss:

1. **Slot check** — Verifies the replication slot from the original clone still exists. If the slot is gone, WAL is unrecoverable and you must start fresh.
2. **Table comparison** — Compares source row counts (from `pg_stat_user_tables`) against actual destination row counts. Tables where `dest_rows < source_rows` are considered incomplete.
3. **Selective re-copy** — Truncates only incomplete tables and re-COPYs them. Complete tables are skipped entirely.
4. **CDC streaming** — Starts WAL streaming from the slot's restart LSN. All WAL accumulated during downtime is replayed — zero data loss.

### Examples

```bash
# Basic clone (schema + data only)
pgmigrator clone --source-dbname=prod --dest-dbname=staging

# Clone with continuous streaming
pgmigrator clone --follow --source-dbname=prod --dest-dbname=staging

# Clone with monitoring
pgmigrator clone --follow --tui --api-port=7654 \
    --source-host=10.0.0.1 --source-dbname=prod \
    --dest-host=10.0.0.2 --dest-dbname=prod

# Clone with 8 parallel workers
pgmigrator clone --copy-workers=8 --source-dbname=prod --dest-dbname=staging

# Resume an interrupted clone (replication slot must still exist)
pgmigrator clone --follow --resume --source-dbname=prod --dest-dbname=staging
```

---

## `follow` — CDC Streaming

### Usage

```bash
pgmigrator follow [flags]
```

### Description

Starts consuming the WAL stream from an existing replication slot and applies changes to the destination in real-time. The replication slot must already exist (typically created by a previous `clone` command).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--start-lsn` | `""` | LSN to start streaming from (e.g., `0/1234ABC`) |
| `--api-port` | `0` | Enable HTTP API/Web UI on this port (0 = disabled) |
| `--tui` | `false` | Show terminal dashboard during streaming |

### Behavior

If `--start-lsn` is provided, the LSN is parsed via `pglogrepl.ParseLSN()` and passed to `pipeline.RunFollow()`. This allows resuming streaming from a known position after a restart.

The `--api-port` and `--tui` flags work identically to the `clone` command.

### Examples

```bash
# Resume streaming (slot already exists)
pgmigrator follow --source-dbname=prod --dest-dbname=staging

# Resume from a specific LSN
pgmigrator follow --start-lsn=0/1A3B4C5 \
    --source-dbname=prod --dest-dbname=staging

# Stream with bidi loop detection
pgmigrator follow --origin-id=pgmigrator-b \
    --source-host=db-a --dest-host=db-b \
    --source-dbname=mydb --dest-dbname=mydb
```

---

## `switchover` — Zero-Downtime Switchover

### Usage

```bash
pgmigrator switchover [flags]
```

### Description

Injects a sentinel message into the replication stream and waits for confirmation that it has been applied to the destination. When the sentinel is confirmed, it proves the destination has applied all WAL changes up to the injection point — the destination is fully caught up and ready to serve traffic.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout` | `30s` | Maximum time to wait for sentinel confirmation |

### Behavior

```go
p := pipeline.New(&cfg, logger)
defer p.Close()
return p.RunSwitchover(cmd.Context(), switchoverTimeout)
```

The switchover:
1. Gets the current applied LSN from the applier
2. Injects a `SentinelMessage` into the pipeline channel
3. Blocks waiting for the applier to confirm the sentinel
4. Returns nil on success (destination is caught up) or an error on timeout

### Examples

```bash
# Standard switchover with 30s timeout
pgmigrator switchover --source-dbname=prod --dest-dbname=staging

# Quick switchover with tight timeout
pgmigrator switchover --timeout=10s \
    --source-dbname=prod --dest-dbname=staging
```

---

## `status` — Migration Progress

### Usage

```bash
pgmigrator status
```

### Description

Displays the current migration state by reading the persisted state file at `~/.pgmigrator/state.json`. Works even when no pipeline is currently running — it shows the last-known state.

### Flags

No additional flags.

### Behavior

1. Reads `~/.pgmigrator/state.json` via `metrics.ReadStateFile()`
2. If the file doesn't exist or can't be read, prints a helpful message
3. Calculates staleness (time since last state write)
4. If the state is older than 10 seconds, marks it as `(stale — Xs ago)`
5. Prints a formatted summary of all metrics

### Output Format

```
Phase:        streaming
Elapsed:      3621s
Applied LSN:  0/1A3B4C5
Confirmed LSN: 0/1A3B4C0
Lag:          1.25 MB (latency: 150ms)
Tables:       42/54 copied
Throughput:   4521 rows/s, 1048576 bytes/s
Total:        12345678 rows, 5368709120 bytes

Tables:
  public.users                       copied  100.0%  (1200000/1200000 rows)
  public.orders                      copying  42.3%  (890000/2100000 rows)
  public.products                    pending   0.0%  (0/450000 rows)
```

When stale:
```
Phase:        streaming (stale — 45s ago)
```

### Examples

```bash
# Check status while a migration is running
pgmigrator status

# Status after pipeline stopped (shows last known state)
pgmigrator status
```

---

## `compare` — Schema Comparison

### Usage

```bash
pgmigrator compare
```

### Description

Compares source and destination schemas to detect drift. **Currently a stub** — prints "compare: not yet implemented".

When implemented, it will use `schema.Migrator.CompareSchemas()` to detect:
- Tables present in source but missing from destination
- Tables present in destination but not in source
- Column type mismatches between source and destination

---

## `serve` — Standalone Web UI Server

### Usage

```bash
pgmigrator serve [flags]
```

### Description

Starts the pgmigrator web UI and REST API server as a standalone process. In this mode, it creates a local `metrics.Collector`, loads the last-known state from the state file, and serves the dashboard. Useful for monitoring after the pipeline has stopped, or for serving the Web UI independently.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `7654` | HTTP server port |

### Behavior

1. Creates a standalone `metrics.Collector`
2. Loads last-known phase from state file (if available)
3. Starts the HTTP server (blocking) with:
   - REST API endpoints (`/api/v1/status`, `/api/v1/tables`, etc.)
   - WebSocket endpoint (`/api/v1/ws`)
   - Embedded React frontend (`/`)
4. Blocks until the context is cancelled (Ctrl+C)

### Examples

```bash
# Start web UI on default port
pgmigrator serve

# Start on custom port
pgmigrator serve --port=8080
```

---

## `tui` — Remote Terminal Dashboard

### Usage

```bash
pgmigrator tui [flags]
```

### Description

Launches a Bubble Tea terminal dashboard that connects to a running pgmigrator instance's HTTP API. This enables monitoring a remote migration from a different terminal or machine.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--api-addr` | `http://localhost:7654` | Address of the pgmigrator API to connect to |

### Behavior

1. Creates a local `metrics.Collector`
2. Starts a background goroutine (`pollRemote`) that:
   - Polls `GET /api/v1/status` every 500ms
   - Updates the local collector with remote snapshot data
   - Records connection errors in the collector
3. Launches the Bubble Tea TUI in the foreground
4. TUI reads from the local collector (which mirrors remote state)

### Remote Polling

```go
func pollRemote(ctx context.Context, addr string, collector *metrics.Collector) {
    client := &http.Client{Timeout: 5 * time.Second}
    ticker := time.NewTicker(500 * time.Millisecond)
    // Polls GET /api/v1/status and updates collector
}
```

The HTTP client has a 5-second timeout per request. If the API is unreachable, errors are recorded in the collector and displayed in the TUI.

### Examples

```bash
# Monitor local instance
pgmigrator tui

# Monitor remote instance
pgmigrator tui --api-addr=http://10.0.0.5:7654
```

---

## Signal Handling

All commands that run pipelines respect context cancellation. When the user presses `Ctrl+C`:

1. Go's signal handler cancels the root context
2. The pipeline's `Run*` methods detect context cancellation
3. Goroutines (decoder, applier, server) shut down gracefully
4. Connections are closed, replication slots remain intact (for resuming later)
5. The state file is written one final time before exit

## Error Handling

All subcommands use `RunE` (returning `error`) rather than `Run`. Errors propagate up to `main()`, which prints them to stderr and exits with code 1. The `SilenceUsage: true` setting on the root command prevents cobra from printing the full usage text on every error, keeping error output clean.

Validation errors from `cfg.Validate()` use `errors.Join()` so multiple problems are reported at once:

```
$ pgmigrator clone
source database name is required
destination database name is required
```
