# pgmigrator

**PostgreSQL online migration tool** — a middleman that owns the WAL stream between source and destination databases, performing parallel initial copy with consistent snapshots and real-time CDC streaming with zero-downtime switchover.

## Why pgmigrator?

Moving a large PostgreSQL database with minimal downtime is hard. Existing tools either require installing extensions on the source, leave artifacts behind, rely on trigger-based replication, or lack fine-grained switchover control.

pgmigrator takes a different approach:

- **Zero database footprint** — only creates a replication slot and publication on the source. No triggers, no sentinel tables, no extensions.
- **Owns the WAL stream** — reads directly from PostgreSQL's logical replication protocol (`pgoutput`), giving full control over the data pipeline.
- **Consistent initial copy** — uses `SET TRANSACTION SNAPSHOT` so all parallel COPY workers see the same point-in-time view, with no gaps or duplicates when CDC streaming starts.
- **Sentinel-based switchover** — injects a synthetic marker into the pipeline. When confirmed, it proves the destination has applied every change up to that point. No lag guessing.
- **Bidirectional replication** — built-in loop detection via PostgreSQL's replication origin tagging, enabling A-to-B and B-to-A replication without infinite loops.

## Features

### Migration Pipeline

```
Source PG ──(repl protocol)──> Decoder ──> [Bidi Filter] ──> Applier ──> Dest PG
                                  │                            │
                            chan Message    ◄── Sentinel Coordinator
                                  │                            │
                          confirms LSN ──> StandbyStatusUpdate ──> Source PG
```

- **Schema migration** — dumps DDL from source via `pg_dump`, applies to destination
- **Parallel COPY** — copies all tables concurrently with configurable worker count, largest tables first for optimal parallelism
- **CDC streaming** — applies INSERT/UPDATE/DELETE in real-time, preserving source transaction boundaries
- **Switchover** — sentinel injection + confirmation for provably-safe cutover

### Monitoring

pgmigrator includes built-in observability with three interfaces:

**Terminal UI (TUI)** — a Bubble Tea dashboard showing phase, lag, per-table progress, throughput, and live logs:

```
┌─ pgmigrator ──────────────────────────────────────────┐
│  Phase: STREAMING    Elapsed: 1h 23m 45s              │
│  Lag: 1.2 MB (150ms)    Throughput: 4,521 rows/s      │
├───────────────────────────────────────────────────────┤
│  Overall: ████████████████████░░░░  78% (42/54 tables)│
├───────────────────────────────────────────────────────┤
│  Table                  Rows       Size     Progress   │
│  public.users           1.2M/1.2M  256 MB   ████ 100%│
│  public.orders          890K/2.1M  1.1 GB   ██░░  42%│
│  public.events          STREAMING  —        ⟳ live   │
├───────────────────────────────────────────────────────┤
│  14:23:01 INF table copy complete table=public.users   │
│  14:23:02 INF starting copy table=public.orders        │
└───────────────────────────────────────────────────────┘
  q: quit  s: switchover  p: pause  r: resume
```

**Web UI** — a React dashboard served from the binary itself (no separate frontend deployment), accessible at the configured API port.

**REST API + WebSocket** — JSON API for status, per-table progress, config, and logs. WebSocket endpoint pushes snapshots every 500ms for real-time updates.

**Offline status** — `pgmigrator status` reads from a persisted state file, so you can check progress even from a different terminal or after the pipeline stops.

## Quick Start

### Install

```bash
# From source
git clone https://github.com/jfoltran/pgmigrator.git
cd pgmigrator
make build

# Or with the installer script
curl -sSL https://raw.githubusercontent.com/jfoltran/pgmigrator/main/scripts/install.sh | bash
```

### Prerequisites

The source database needs logical replication enabled:

```sql
-- postgresql.conf (or ALTER SYSTEM)
wal_level = logical
max_replication_slots = 4
max_wal_senders = 4
```

Create a publication for the tables you want to migrate:

```sql
CREATE PUBLICATION pgmigrator_pub FOR ALL TABLES;
```

### Run a Migration

```bash
# Full clone: schema + parallel COPY + CDC streaming
pgmigrator clone --follow \
    --source-uri="postgres://replication_user:$SOURCE_PW@source.db.internal/production" \
    --dest-uri="postgres://migration_user:$DEST_PW@dest.db.internal/production"

# With monitoring
pgmigrator clone --follow --tui \
    --source-uri="postgres://postgres@source.db.internal/production" \
    --dest-uri="postgres://postgres@dest.db.internal/production"

# Or with the web UI
pgmigrator clone --follow --api-port=7654 \
    --source-uri="postgres://postgres@source.db.internal/production" \
    --dest-uri="postgres://postgres@dest.db.internal/production"
```

### Switchover

When you're ready to cut over traffic to the destination:

```bash
pgmigrator switchover --timeout=30s \
    --source-uri="postgres://postgres@source.db.internal/production" \
    --dest-uri="postgres://postgres@dest.db.internal/production"
```

This injects a sentinel, waits for the destination to confirm it has applied everything, and exits. At that point, the destination is guaranteed to be fully caught up.

## Commands

| Command | Description |
|---------|-------------|
| `clone` | Copy schema and data from source to destination |
| `clone --follow` | Clone then transition to real-time CDC streaming |
| `follow` | Stream CDC changes (replication slot must already exist) |
| `switchover` | Inject sentinel and wait for confirmation |
| `status` | Show migration progress from the state file |
| `serve` | Start standalone web UI server |
| `tui` | Launch terminal dashboard (connects to running instance) |
| `compare` | Compare source and destination schemas *(stub)* |

## Docker

```bash
# Build
docker build -t pgmigrator .

# Run with docker-compose (includes source + dest PostgreSQL)
docker compose up
```

The `docker-compose.yml` spins up a complete test environment: source PostgreSQL (with `wal_level=logical`), destination PostgreSQL, and pgmigrator with the web UI on port 7654.

## Architecture

```
cmd/pgmigrator/        CLI commands (cobra)
internal/
├── pipeline/          Orchestration — wires all components, manages lifecycle
├── stream/            Message interface + WAL Decoder (pglogrepl)
├── replay/            Applier — applies DML to destination via pgx
├── sentinel/          Sentinel coordinator for zero-downtime switchover
├── snapshot/          Parallel COPY with consistent snapshots
├── schema/            DDL dump/apply/compare via pg_dump
├── pgwire/            PG protocol helpers (replication origin, slot mgmt)
├── bidi/              Bidirectional replication loop detection
├── config/            Typed configuration with validation
├── metrics/           Metrics collector + state file persistence
├── server/            HTTP server (REST API + WebSocket + embedded frontend)
└── tui/               Bubble Tea terminal dashboard
pkg/lsn/              Public LSN utilities (lag calculation, formatting)
web/                   React + TypeScript + Vite frontend
scripts/               Installer script
```

### Key Design Decisions

- **Unified `Message` channel** — WAL changes and sentinels flow through the same `chan Message`. No separate control channels, no coordination complexity.
- **pgoutput plugin** — built-in since PostgreSQL 10, binary protocol, publication-aware. No `wal2json` dependency.
- **Origin tagging** — the decoder captures `OriginMessage` from the WAL, the applier tags writes via `pg_replication_origin_session_setup()`, and the bidi filter drops echoed changes.
- **In-memory pipeline** — no intermediate storage. WAL events flow from decoder to applier through Go channels with back-pressure.

## Build

```bash
make build        # Go binary only (fast, uses placeholder frontend)
make build-full   # Frontend + Go binary (production)
make test         # Run all tests
make lint         # go vet
make docker       # Build Docker image
```

## Requirements

- **Go 1.24+**
- **PostgreSQL 10+** on source (for logical replication with pgoutput)
- **`pg_dump`** in PATH (for schema migration)
- **Node.js 20+** (only needed for building the web UI frontend)

## Documentation

Detailed documentation for every component is in [`docs/`](docs/):

| Document | Coverage |
|----------|----------|
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
