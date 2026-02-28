# Testing

migrator has four test tiers: **unit**, **integration** (app-level), **integration** (migration pipeline), and **benchmark**. Unit tests run without external dependencies. Everything else requires Docker or Podman.

## Prerequisites

- Go 1.24+
- Docker **or** Podman (for integration/benchmark tests)
- Node 22+ or bun (for frontend type-checking only)

Container runtime detection: `docker` on PATH → `podman` on PATH. Override with the `CONTAINER_RUNTIME` env var.

## Quick Reference

```bash
make test                # unit tests — no containers needed
make test-integration    # migration pipeline integration tests (source/dest PG containers)
make test-benchmark      # 25GB benchmark against tuned PG containers
make test-stop           # tear down all test containers
go test -tags=integration -run TestStoreAddAndGet ./internal/cluster/  # single integration test
```

## Unit Tests

No database, no containers. Runs everywhere.

```bash
make test
# or:
go test ./... -v -count=1
```

### What's covered

| Package | Tests |
|---------|-------|
| `internal/config` | DSN building, validation, defaults |
| `internal/metrics` | Phase tracking, table lifecycle, log buffer, sliding window |
| `internal/migration/bidi` | Loop-detection filter, origin matching, context cancellation |
| `internal/migration/replay` | SQL generation (INSERT/UPDATE/DELETE, identifier quoting) |
| `internal/migration/schema` | Statement splitting (incl. dollar-quoted PL/pgSQL), schema diff |
| `internal/migration/sentinel` | Sentinel message interface, coordinator initiate/confirm/timeout |
| `internal/migration/snapshot` | Table info, identifier quoting |
| `internal/migration/stream` | Message types, kind/op stringers, origin extraction |
| `internal/server` | HTTP handlers (status, tables, config, logs, CORS) |
| `internal/cluster` | `ValidateCluster` pure validation (no DB) |
| `pkg/lsn` | Lag calculation, human-readable formatting |

> Tests with `//go:build integration` or `//go:build benchmark` tags are automatically excluded from `go test ./...`.

## App-Level Integration Tests

These test the PostgreSQL-backed cluster store and HTTP API handlers against a real database. They use the `//go:build integration` tag and require a running PostgreSQL instance.

### Running with Docker Compose

The easiest way is to use the app's `docker-compose.yml` which already has a PostgreSQL instance:

```bash
# Start the backend database
docker compose up db -d --wait

# Run integration tests against it
PGMANAGER_TEST_DB_URL="postgres:./pgmanager:migrator@localhost:543./pgmanager?sslmode=disable" \
  go test -tags=integration -v -count=1 ./internal/cluster/ ./internal/server/
```

Or use any PostgreSQL instance — just set `PGMANAGER_TEST_DB_URL`:

```bash
PGMANAGER_TEST_DB_URL="postgres://user:pass@localhost:5432/testdb?sslmode=disable" \
  go test -tags=integration -v -count=1 ./internal/cluster/ ./internal/server/
```

### What's covered

| Package | Tests |
|---------|-------|
| `internal/cluster` | Full store CRUD (Add, Get, List, Update, Remove), node management (AddNode, RemoveNode), duplicate detection, not-found errors |
| `internal/server` | HTTP handler CRUD (POST/GET/PUT/DELETE clusters), validation (400), conflict (409), not-found (404) |

### How it works

Each test creates fresh `clusters` and `nodes` tables via `setupTestStore()` / `setupClusterTest()`, dropping any existing tables first. Tests are fully isolated — they don't rely on the `db.Open()` migration system.

## Migration Pipeline Integration Tests

End-to-end tests that exercise the full migration pipeline (schema copy → parallel snapshot → CDC streaming) against real PostgreSQL 18 containers.

```bash
make test-integration
```

This will:
1. Start two PG 18 containers via `docker-compose.test.yml` (source on `:55432`, dest on `:55433`)
2. Run tests tagged `//go:build integration` in `internal/migration/pipeline/`
3. Tear down containers automatically (even on Ctrl+C)

**Run a single test:**
```bash
make test-integration RUN=TestIntegration_CDCStreaming
```

**Manual container management** (useful during development):
```bash
docker compose -f docker-compose.test.yml up -d
go test -tags=integration -v -count=1 -timeout=300s ./internal/migration/pipeline/
docker compose -f docker-compose.test.yml down -v
```

### Container Configuration

| Setting | Value |
|---------|-------|
| Source DSN | `postgres://postgres:source@localhost:55432/source` |
| Dest DSN | `postgres://postgres:dest@localhost:55433/dest` |
| WAL level | `logical` (source only) |
| PG version | 18 |

## Benchmark Tests

Large-scale performance tests that generate ~25GB of data across 5 tables and measure clone/CDC throughput.

```bash
make test-benchmark
```

This will:
1. Start two tuned PG 18 containers via `docker-compose.bench.yml`
2. Run tests tagged `//go:build benchmark` (4-hour timeout)
3. Tear down containers automatically

**Run a specific benchmark:**
```bash
make test-benchmark RUN=Clone25GB
make test-benchmark RUN=CloneAndFollow25GB
```

### Available Benchmarks

| Test | Description | Duration |
|------|-------------|----------|
| `TestBenchmark_Clone25GB` | Seeds 5 tables (~25GB total), clones all to destination, verifies row counts | 15–30 min |
| `TestBenchmark_CloneAndFollow25GB` | Seeds 1 table (~5GB), clones, then streams 100K CDC inserts and verifies convergence | 10–20 min |

### Benchmark Container Tuning

The benchmark compose (`docker-compose.bench.yml`) uses performance-tuned PostgreSQL:

| Setting | Value | Why |
|---------|-------|-----|
| `shared_buffers` | 512MB | Larger buffer pool for bulk operations |
| `work_mem` | 64MB | Larger sort/hash memory |
| `maintenance_work_mem` | 256MB | Faster ANALYZE/index builds |
| `max_wal_size` | 2GB | Fewer checkpoints during bulk insert |
| `checkpoint_timeout` | 30min | Defer checkpoints during seeding |
| `shm_size` | 1GB | Shared memory for PG |

### Seeding Strategy

- **UNLOGGED tables** during insert (skips WAL), converted to LOGGED after seeding
- **4 parallel workers** per table, each with `synchronous_commit = off`
- **100K row batches** to balance WAL pressure and commit overhead
- **All 5 tables seeded concurrently**
- Progress reported every 10 seconds

## Troubleshooting

### Containers won't start

```bash
make test-stop                    # clean up orphaned containers
docker ps -a | grep postgres      # check for port conflicts
```

### Tests hang

If containers from a previous run are in a bad state:

```bash
make test-stop
make test-integration             # fresh start
```

### Port conflicts

Pipeline integration tests use `:55432` (source) and `:55433` (dest). App integration tests use whichever port your backend PG runs on. Stop any conflicting local PostgreSQL:

```bash
# macOS
brew services stop postgresql@18
# Linux
sudo systemctl stop postgresql
```

### Podman users

```bash
export CONTAINER_RUNTIME=podman
make test-integration
```

The Makefile auto-detects `docker compose` → `podman-compose` → `podman compose`.
