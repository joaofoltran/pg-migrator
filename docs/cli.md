# CLI Commands

**Package:** `cm./pgmanager`
**Files:** `main.go`, `root.go`, `clone.go`, `follow.go`, `switchover.go`, `status.go`, `compare.go`, `serve.go`, `daemon.go`, `logs.go`, `cluster.go`, `tui.go`

## Overview

pgmanager uses [cobra](https://github.com/spf13/cobra) for its CLI framework. The root command defines global persistent flags (database connections, replication settings, logging), and each subcommand implements a specific operation. All commands share the same `config.Config` struct and `zerolog.Logger`, initialized in the root command's `PersistentPreRunE`.

## Command Tree

```
pgmanager
├── daemon
│   ├── start       Launch the background daemon (API + Web UI)
│   ├── stop        Stop the daemon
│   └── status      Show daemon status
├── cluster
│   ├── add         Register a new PostgreSQL cluster
│   ├── list (ls)   List registered clusters
│   ├── show        Show cluster details
│   ├── remove (rm) Remove a registered cluster
│   └── test        Test connectivity to cluster nodes
├── clone           Copy schema + data, optionally follow with CDC
├── follow          Stream CDC changes from source to destination
├── switchover      Zero-downtime switchover via sentinel marker
├── status          Show migration progress
├── compare         Compare source and destination schemas (stub)
├── serve           Start standalone web UI server (deprecated → daemon start)
├── logs            Tail daemon log entries
└── tui             Launch terminal dashboard
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

### Configuration

| Setting | Value | Purpose |
|---------|-------|---------|
| `SilenceUsage` | `true` | Don't print usage on errors |
| `SilenceErrors` | `true` | Errors are printed by `main()` instead |

### Logger Initialization (`PersistentPreRunE`)

Runs before every subcommand. Configures zerolog based on `--log-format` and `--log-level`:

| Format | Behavior |
|--------|----------|
| `"json"` | `zerolog.New(os.Stdout)` — JSON lines to stdout |
| `"console"` (default) | `zerolog.ConsoleWriter{Out: os.Stderr}` — Colored, human-readable to stderr |

### Global Flags

All flags are **persistent** (inherited by all subcommands):

#### Source Database

| Flag | Default | Description |
|------|---------|-------------|
| `--source-uri` | `""` | Source connection URI (e.g. `postgres://user:pass@host:5432/dbname`) |
| `--source-host` | `localhost` | Source PostgreSQL host |
| `--source-port` | `5432` | Source PostgreSQL port |
| `--source-user` | `postgres` | Source PostgreSQL user |
| `--source-password` | `""` | Source PostgreSQL password |
| `--source-dbname` | `""` | Source database name (**required** for pipeline commands) |

#### Destination Database

| Flag | Default | Description |
|------|---------|-------------|
| `--dest-uri` | `""` | Destination connection URI |
| `--dest-host` | `localhost` | Destination PostgreSQL host |
| `--dest-port` | `5432` | Destination PostgreSQL port |
| `--dest-user` | `postgres` | Destination PostgreSQL user |
| `--dest-password` | `""` | Destination PostgreSQL password |
| `--dest-dbname` | `""` | Destination database name (**required** for pipeline commands) |

#### Replication

| Flag | Default | Description |
|------|---------|-------------|
| `--slot` | `pgmanager` | Replication slot name |
| `--publication` | `pgmanager_pub` | Publication name |
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

## `daemon` — Background Daemon Management

### `daemon start`

Launches the pgmanager daemon as a background process. The daemon serves the HTTP API, Web UI, and accepts pipeline jobs via the API. Cluster management routes are also registered.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `7654` | HTTP API/Web UI port |
| `--foreground` | `false` | Run in foreground (for containers/systemd) |

**Behavior:**

1. If not `--foreground`, checks for existing daemon via PID file
2. Re-execs the binary with `_PGMANAGER_DAEMON=1` env and `Setsid: true`
3. In daemon mode: writes PID, initializes logger, creates metrics collector, job manager, cluster store, and HTTP server
4. Registers all routes: status/tables/config/logs, WebSocket, job control, cluster CRUD
5. Blocks on signal (SIGTERM/SIGINT) for graceful shutdown

**Examples:**

```bash
pgmanager daemon start                     # Background daemon
pgmanager daemon start --foreground        # Foreground (containers)
pgmanager daemon start --port 8080         # Custom port
```

### `daemon stop`

Sends SIGTERM to the daemon process. Waits up to 30 seconds, then SIGKILL.

### `daemon status`

Shows whether the daemon is running, its PID, and API address.

---

## `cluster` — Multi-Cluster Management

### `cluster add`

Register a new PostgreSQL cluster by providing connection details for its nodes.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--id` | (required) | Cluster identifier |
| `--name` | (required) | Cluster display name |
| `--node` | (required) | Node spec: `role:host:port` (repeatable) |
| `--tag` | `[]` | Tags (e.g. `env:prod`) (repeatable) |

**Node roles:** `primary`, `replica`, `standby`

**Examples:**

```bash
# Single-node cluster
pgmanager cluster add --id prod --name "Production" \
    --node primary:10.0.0.1:5432

# Multi-node cluster with tags
pgmanager cluster add --id staging --name "Staging" \
    --node primary:pg-staging.local:5432 \
    --node replica:pg-replica.local:5432 \
    --tag env:staging --tag region:us-east
```

### `cluster list` (alias: `ls`)

List all registered clusters in tabular format.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON |

**Output:**

```
ID       NAME        NODES                 TAGS         CREATED
prod     Production  1 primary, 1 replica  env:prod     2026-02-28 01:53
staging  Staging     1 primary             env:staging  2026-02-28 02:10
```

### `cluster show [cluster-id]`

Show detailed information for a cluster including all nodes.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON |

**Output:**

```
Cluster: Production (prod)
Tags:    env:prod
Created: 2026-02-28 01:53:55 UTC
Updated: 2026-02-28 01:53:55 UTC

Nodes (2):
  ID       ROLE     HOST        PORT  AGENT
  primary  primary  10.0.0.1    5432  -
  replica  replica  10.0.0.2    5432  -
```

### `cluster remove [cluster-id]` (alias: `rm`)

Remove a registered cluster from the store.

### `cluster test [cluster-id]`

Test connectivity to all nodes in a cluster. Validates reachability, PostgreSQL version, replica status, and user privileges.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--user` | `postgres` | PostgreSQL user for connection test |
| `--password` | `""` | PostgreSQL password for connection test |
| `--dbname` | `postgres` | Database name for connection test |

**Output:**

```
Testing 2 node(s) in cluster "prod"...

  [OK] primary (10.0.0.1:5432) - primary
         Version: PostgreSQL 16.2
         Latency: 2.3ms
         Replica: false
         Privileges: connect, replication

  [OK] replica (10.0.0.2:5432) - replica
         Version: PostgreSQL 16.2
         Latency: 3.1ms
         Replica: true
         Privileges: connect
```

---

## `clone` — Full Database Copy

### Usage

```bash
pgmanager clone [flags]
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
| `--foreground` | `false` | Run directly instead of submitting to daemon |
| `--tui` | `false` | Show terminal dashboard during migration |

### Behavior

When a daemon is running, `clone` auto-detects it via `client.Ping()` and submits a `ClonePayload` to the daemon API. The pipeline runs in the daemon process while the CLI returns immediately.

With `--foreground`, the pipeline runs in the current process with a local metrics collector and optional TUI/API server.

### Resume Mode (`--resume --follow`)

When a clone is interrupted, `--resume` recovers without data loss:

1. **Slot check** — Verifies the replication slot still exists
2. **Table comparison** — Compares source vs destination row counts
3. **Selective re-copy** — Truncates only incomplete tables and re-COPYs them
4. **CDC streaming** — Starts WAL streaming from the slot's restart LSN

### Examples

```bash
# Clone with continuous streaming (daemon mode)
pgmanager clone --follow \
    --source-uri="postgres://user:pass@source:5432/prod" \
    --dest-uri="postgres://user:pass@dest:5432/prod"

# Clone with 8 parallel workers
pgmanager clone --follow --copy-workers=8 \
    --source-uri="postgres://user:pass@source:5432/prod" \
    --dest-uri="postgres://user:pass@dest:5432/prod"

# Resume an interrupted clone
pgmanager clone --follow --resume \
    --source-uri="postgres://user:pass@source:5432/prod" \
    --dest-uri="postgres://user:pass@dest:5432/prod"

# Foreground with TUI
pgmanager clone --follow --foreground --tui \
    --source-dbname=prod --dest-dbname=staging
```

---

## `follow` — CDC Streaming

### Usage

```bash
pgmanager follow [flags]
```

### Description

Starts consuming the WAL stream from an existing replication slot and applies changes to the destination in real-time. The replication slot must already exist (typically created by a previous `clone` command).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--start-lsn` | `""` | LSN to start streaming from (e.g., `0/1234ABC`) |
| `--foreground` | `false` | Run directly instead of submitting to daemon |
| `--tui` | `false` | Show terminal dashboard during streaming |

### Examples

```bash
# Resume streaming (daemon mode)
pgmanager follow \
    --source-uri="postgres://user:pass@source:5432/prod" \
    --dest-uri="postgres://user:pass@dest:5432/prod"

# Stream from a specific LSN
pgmanager follow --start-lsn=0/1A3B4C5 \
    --source-uri="postgres://user:pass@source:5432/prod" \
    --dest-uri="postgres://user:pass@dest:5432/prod"
```

---

## `switchover` — Zero-Downtime Switchover

### Usage

```bash
pgmanager switchover [flags]
```

### Description

Injects a sentinel message into the replication stream and waits for confirmation. When confirmed, the destination has applied all WAL changes up to the injection point.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout` | `30s` | Maximum time to wait for sentinel confirmation |
| `--foreground` | `false` | Run directly instead of submitting to daemon |

### Examples

```bash
pgmanager switchover --timeout=30s \
    --source-uri="postgres://user:pass@source:5432/prod" \
    --dest-uri="postgres://user:pass@dest:5432/prod"
```

---

## `status` — Migration Progress

### Usage

```bash
pgmanager status
```

### Description

Queries the daemon API first (`GET /api/v1/status`). If the daemon is not running, falls back to reading the persisted state file at `~/.pgmanager/state.json`.

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

---

## `logs` — Daemon Log Entries

### Usage

```bash
pgmanager logs [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-f, --follow` | `false` | Follow log output (like `tail -f`) |

### Description

Tails the daemon log file at `~/.pgmanager/pgmanager.log`. With `--follow`, polls for new entries continuously.

---

## `tui` — Remote Terminal Dashboard

### Usage

```bash
pgmanager tui [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--api-addr` | `http://localhost:7654` | Address of the pgmanager API |

### Description

Launches a Bubble Tea terminal dashboard that connects to the daemon's HTTP API. Polls `GET /api/v1/status` every 500ms and renders phase, lag, per-table progress, throughput, and live logs.

### Examples

```bash
pgmanager tui                                    # Monitor local daemon
pgmanager tui --api-addr=http://10.0.0.5:7654    # Monitor remote daemon
```

---

## `serve` — Standalone Web UI Server (Deprecated)

Redirects to `daemon start --foreground`. Kept for backward compatibility.

---

## Signal Handling

All commands that run pipelines respect context cancellation. On `Ctrl+C`:

1. Go's signal handler cancels the root context
2. Active jobs are stopped gracefully
3. Connections are closed, replication slots remain intact
4. The state file is written one final time before exit

## Error Handling

All subcommands use `RunE` (returning `error`) rather than `Run`. Errors propagate up to `main()`, which prints them to stderr and exits with code 1. Validation errors from `cfg.Validate()` use `errors.Join()` so multiple problems are reported at once.
