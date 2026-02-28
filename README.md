# pgmanager

**PostgreSQL administration suite** — multi-cluster management with online migration, backup, standby management, monitoring, and cluster administration.

## Why pgmanager?

Managing PostgreSQL clusters at scale is hard. Existing tools are fragmented — one for migration, another for backup, another for monitoring — each with different concepts, interfaces, and deployment models.

pgmanager takes a unified approach:

- **Multi-cluster management** — register and manage multiple PostgreSQL clusters from a single daemon, agentless by default
- **Zero database footprint** — only creates a replication slot and publication on the source. No triggers, no sentinel tables, no extensions
- **Least privilege** — every module operates with the minimum PostgreSQL privileges required. Never assumes superuser
- **Owns the WAL stream** — reads directly from PostgreSQL's logical replication protocol (`pgoutput`), giving full control over the data pipeline
- **Consistent initial copy** — uses `SET TRANSACTION SNAPSHOT` so all parallel COPY workers see the same point-in-time view, with no gaps or duplicates when CDC streaming starts
- **Sentinel-based switchover** — injects a synthetic marker into the pipeline. When confirmed, it proves the destination has applied every change up to that point
- **Bidirectional replication** — built-in loop detection via PostgreSQL's replication origin tagging
- **Daemon architecture** — runs as a background service with an HTTP API, Web UI, and CLI. Manages clusters, jobs, and monitoring from a single process

## Features

### Multi-Cluster Management

```
Central Daemon (pgmanager daemon)
  │
  ├── Cluster A (agentless — libpq only)
  │     ├── primary node
  │     └── replica node
  │
  └── Cluster B (with optional agent)
        ├── primary node  ◀── pgmanager agent
        └── standby node  ◀── pgmanager agent
```

- **Register clusters** via CLI (`pgmanager cluster add`) or REST API (`POST /api/v1/clusters`)
- **Agentless by default** — everything works over libpq. Optional agent for filesystem-level operations
- **Connection testing** — validates reachability, PG version, replica status, and privilege levels
- **Cluster-scoped jobs** — migration, backup, and standby operations are tied to specific clusters

### Migration Pipeline

```
Source PG ──(repl protocol)──> Decoder ──> [Bidi Filter] ──> Applier ──> Dest PG
                                  │                            │
                            chan Message    ◄── Sentinel Coordinator
                                  │                            │
                          confirms LSN ──> StandbyStatusUpdate ──> Source PG
```

- **Schema migration** — dumps DDL from source via `pg_dump`, applies to destination
- **Parallel COPY** — copies all tables concurrently with configurable worker count, largest tables first
- **CDC streaming** — applies INSERT/UPDATE/DELETE in real-time, preserving source transaction boundaries
- **Switchover** — sentinel injection + confirmation for provably-safe cutover

### Monitoring

pgmanager includes built-in observability with three interfaces:

**Web UI** — a React dashboard with sidebar navigation, cluster management, migration monitoring, and module switching. Served from the binary itself at port 7654.

**Terminal UI (TUI)** — a Bubble Tea dashboard showing phase, lag, per-table progress, throughput, and live logs.

**REST API + WebSocket** — JSON API for status, per-table progress, cluster management, config, and logs. WebSocket endpoint pushes snapshots every 500ms.

**Offline status** — `pgmanager status` reads from a persisted state file, so you can check progress even from a different terminal.

## Quick Start

### Install

```bash
# From source
git clone https://github.com/jfoltran/pgmanager.git
cd pgmanager
make build

# Or with the installer script
curl -sSL https://raw.githubusercontent.com/jfoltra./pgmanager/main/scripts/install.sh | bash
```

### Prerequisites

The source database needs logical replication enabled:

```sql
-- postgresql.conf (or ALTER SYSTEM)
wal_level = logical
max_replication_slots = 4
max_wal_senders = 4
```

### Usage

#### Register a cluster

```bash
pgmanager cluster add --id prod --name "Production" \
    --node primary:source-pg.local:5432 \
    --node replica:replica-pg.local:5432 \
    --tag env:prod

pgmanager cluster list
pgmanager cluster test prod --user postgres --password secret
```

#### Start the daemon

```bash
pgmanager daemon start
```

#### Submit a migration job

```bash
pgmanager clone --follow \
    --source-uri="postgres://user:pass@source:5432/production" \
    --dest-uri="postgres://user:pass@dest:5432/production"
```

#### Monitor progress

```bash
pgmanager status
pgmanager tui
pgmanager logs -f
# Or open http://localhost:7654 in your browser
```

#### Cut over

```bash
pgmanager switchover --timeout=30s \
    --source-uri="postgres://user:pass@source:5432/production" \
    --dest-uri="postgres://user:pass@dest:5432/production"
```

#### Stop the daemon

```bash
pgmanager daemon stop
```

#### Foreground mode (for CI / containers)

```bash
pgmanager clone --follow --foreground \
    --source-uri="postgres://user:pass@source:5432/production" \
    --dest-uri="postgres://user:pass@dest:5432/production"
```

## Commands

| Command | Description |
|---------|-------------|
| `daemon start` | Start the background daemon (API + Web UI on port 7654) |
| `daemon stop` | Stop the daemon |
| `daemon status` | Show daemon status |
| `cluster add` | Register a new PostgreSQL cluster |
| `cluster list` | List registered clusters |
| `cluster show` | Show cluster details (nodes, tags, timestamps) |
| `cluster remove` | Remove a registered cluster |
| `cluster test` | Test connectivity and privileges for all nodes in a cluster |
| `clone` | Copy schema and data from source to destination |
| `clone --follow` | Clone then transition to real-time CDC streaming |
| `follow` | Stream CDC changes (replication slot must already exist) |
| `switchover` | Inject sentinel and wait for confirmation |
| `status` | Show migration progress (queries daemon, falls back to state file) |
| `logs [-f]` | Show daemon log entries |
| `tui` | Launch terminal dashboard (connects to daemon API) |
| `compare` | Compare source and destination schemas *(stub)* |

All pipeline commands (`clone`, `follow`, `switchover`) auto-detect a running daemon and submit via API. Use `--foreground` to bypass the daemon and run directly.

## Docker

```bash
# Build
docker build -t pgmanager .

# Run with docker-compose (includes source + dest PostgreSQL)
docker compose up
```

## Architecture

```
cm./pgmanager/               CLI commands (cobra)
internal/
├── cluster/                Shared — Cluster/Node data model, JSON store, connection testing
├── config/                 Shared — connection config, validation
├── daemon/                 Shared — PID, re-exec, JobManager, HTTP client
├── server/                 Shared — HTTP server, REST API, WebSocket, cluster/job handlers
├── metrics/                Shared — collector, state persister, log writer
├── tui/                    Shared — Bubble Tea terminal dashboard
├── testutil/               Shared — test helpers
└── migration/              Migration module
    ├── pipeline/           Orchestration — wires all components, manages lifecycle
    ├── stream/             Message interface + WAL Decoder (pglogrepl)
    ├── replay/             Applier — applies DML to destination via pgx
    ├── sentinel/           Sentinel coordinator for zero-downtime switchover
    ├── snapshot/           Parallel COPY with consistent snapshots
    ├── schema/             DDL dump/apply/compare via pg_dump
    ├── pgwire/             PG protocol helpers (replication origin, slot mgmt)
    └── bidi/               Bidirectional replication loop detection
pkg/lsn/                   Public LSN utilities (lag calculation, formatting)
web/                        React + TypeScript + Vite frontend
```

Shared infrastructure (`cluster`, `config`, `daemon`, `server`, `metrics`, `tui`) lives at the `internal/` root. Domain-specific packages are namespaced under `internal/migration/`, ready for future modules (`internal/backup/`, `internal/standby/`, etc.).

### Key Design Decisions

- **Security first** — passwords are never logged or exposed via API. Connection testing validates privilege levels. Cluster store uses 0600 file permissions
- **Least privilege** — every module documents and validates minimum required PG roles. Never assumes superuser
- **Daemon-first** — pipeline runs in a background daemon. CLI commands are thin API clients
- **Multi-cluster** — clusters registered via JSON store at `~/.pgmanager/clusters.json`. CRUD via CLI and REST API
- **Hybrid agent model** — agentless by default (libpq), optional stateless agent for filesystem operations
- **Unified `Message` channel** — WAL changes and sentinels flow through the same `chan Message`
- **pgoutput plugin** — built-in since PostgreSQL 10, binary protocol, publication-aware. No `wal2json` dependency
- **Origin tagging** — the decoder captures `OriginMessage`, the applier tags writes via `pg_replication_origin_session_setup()`, the bidi filter drops echoes
- **In-memory pipeline** — no intermediate storage. WAL events flow from decoder to applier through Go channels with back-pressure

## API

### Cluster Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/clusters` | List all registered clusters |
| `POST` | `/api/v1/clusters` | Register a new cluster |
| `GET` | `/api/v1/clusters/{id}` | Get cluster details |
| `PUT` | `/api/v1/clusters/{id}` | Update cluster |
| `DELETE` | `/api/v1/clusters/{id}` | Remove cluster |
| `POST` | `/api/v1/clusters/test-connection` | Test database connectivity |

### Migration Monitoring

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/status` | Current migration snapshot |
| `GET` | `/api/v1/tables` | Per-table progress |
| `GET` | `/api/v1/config` | Redacted configuration |
| `GET` | `/api/v1/logs` | Log entries |
| `WS` | `/api/v1/ws` | Real-time snapshot push (500ms) |

### Job Control

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/jobs/clone` | Submit clone job |
| `POST` | `/api/v1/jobs/follow` | Submit follow job |
| `POST` | `/api/v1/jobs/switchover` | Submit switchover job |
| `POST` | `/api/v1/jobs/stop` | Stop running job |
| `GET` | `/api/v1/jobs/status` | Check job status |

## Build

```bash
make build        # Go binary only
make build-full   # Frontend + Go binary (production)
make test         # Run all tests
make lint         # go vet
make docker       # Build Docker image
```

## Requirements

- **Go 1.24+**
- **PostgreSQL 10+** on source (for logical replication with pgoutput)
- **`pg_dump`** in PATH (for schema migration)
- **Bun or Node.js 20+** (only needed for building the web UI frontend)

## Documentation

Detailed documentation for every component is in [`docs/`](docs/):

| Document | Coverage |
|----------|----------|
| [Cluster](docs/cluster.md) | Multi-cluster management, JSON store, connection testing |
| [Pipeline](docs/pipeline.md) | Orchestration, phases, run methods |
| [Stream & Decoder](docs/stream.md) | Message interface, WAL decoding |
| [Applier](docs/replay.md) | DML generation, transaction boundaries |
| [Sentinel](docs/sentinel.md) | Switchover mechanism |
| [Snapshot](docs/snapshot.md) | Parallel COPY, snapshot consistency |
| [Schema](docs/schema.md) | DDL dump/apply/compare |
| [Bidi](docs/bidi.md) | Loop detection for bidirectional replication |
| [PG Wire](docs/pgwire.md) | Replication origin, slot management |
| [Config](docs/config.md) | Configuration structs, DSN builders |
| [LSN Utilities](docs/lsn.md) | Lag calculation, formatting |
| [Metrics](docs/metrics-collector.md) | Metrics collector, state persistence |
| [HTTP Server](docs/http-server.md) | REST API, WebSocket, embedded frontend |
| [TUI](docs/tui.md) | Terminal dashboard components |
| [Web UI](docs/web-ui.md) | React frontend architecture |
| [CLI](docs/cli.md) | All commands, flags, examples |
| [Installer & Docker](docs/installer.md) | Shell installer, Dockerfile, Makefile |

## License

MIT
