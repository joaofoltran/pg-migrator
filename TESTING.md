# Testing

migrator has three test tiers: **unit**, **integration**, and **benchmark**. Unit tests run without any external dependencies. Integration and benchmark tests require Docker or Podman to spin up PostgreSQL containers.

## Prerequisites

- Go 1.24+
- Docker **or** Podman (for integration/benchmark tests only)

Container runtime detection order: `$CONTAINER_RUNTIME` env → `docker` on PATH → `podman` on PATH.

## Quick Reference

```bash
make test                # unit tests (no containers needed)
make test-integration    # integration tests against live PG containers
make test-benchmark      # 25GB benchmark against tuned PG containers
make test-stop           # force-stop all test containers
```

## Unit Tests

Run all unit tests (no database required):

```bash
make test
# or directly:
go test ./... -v
```

Coverage spans 12 packages:

| Package | What's tested |
|---------|---------------|
| `internal/cluster` | Store CRUD, node management, persistence, file permissions, validation |
| `internal/migration/bidi` | Loop-detection filter, origin matching, context cancellation |
| `internal/config` | DSN building, validation, defaults |
| `internal/metrics` | Phase tracking, table lifecycle, log buffer, sliding window |
| `internal/migration/replay` | SQL generation (INSERT/UPDATE/DELETE parts, quoting) |
| `internal/migration/schema` | Statement splitting (incl. dollar-quoted PL/pgSQL), schema diff |
| `internal/migration/sentinel` | Sentinel message interface, coordinator initiate/confirm/timeout |
| `internal/server` | HTTP handlers (status, tables, config, logs, CORS), cluster CRUD API |
| `internal/migration/snapshot` | Table info, identifier quoting |
| `internal/migration/stream` | Message types, kind/op stringers, origin extraction |
| `pkg/lsn` | Lag calculation, human-readable formatting |

## Integration Tests

End-to-end tests that exercise the full pipeline (schema copy → snapshot → CDC streaming) against real PostgreSQL 18 containers.

```bash
make test-integration
```

This will:
1. Start two PG 18 containers via `docker-compose.test.yml` (source on `:5432`, dest on `:5433`)
2. Run tests tagged with `//go:build integration`
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

The test suite has its own `TestMain` that auto-starts containers if the databases aren't reachable, so `go test` works even without running compose first — it just needs Docker/Podman available.

### Container Configuration

| Setting | Value |
|---------|-------|
| Source DSN | `postgres://postgres:source@localhost:5432/source` |
| Dest DSN | `postgres://postgres:dest@localhost:5433/dest` |
| WAL level | `logical` (source only) |
| PG version | 18 |

## Benchmark Tests

Large-scale performance tests that generate ~25GB of data across 5 tables and measure clone/CDC throughput.

```bash
make test-benchmark
```

This will:
1. Start two tuned PG 18 containers via `docker-compose.bench.yml`
2. Run tests tagged with `//go:build benchmark` (4-hour timeout)
3. Tear down containers automatically

**Run a specific benchmark:**
```bash
make test-benchmark RUN=Clone25GB           # 5-table parallel clone only
make test-benchmark RUN=CloneAndFollow25GB  # single-table clone + CDC test
```

### Benchmark Tests Available

| Test | Description | Duration |
|------|-------------|----------|
| `TestBenchmark_Clone25GB` | Seeds 5 tables (~25GB total), clones all to destination, verifies row counts | 15–30 min |
| `TestBenchmark_CloneAndFollow25GB` | Seeds 1 table (~5GB), clones, then streams 100K CDC inserts and verifies convergence | 10–20 min |

### Benchmark Container Tuning

The benchmark compose file (`docker-compose.bench.yml`) uses performance-tuned PostgreSQL settings:

| Setting | Value | Why |
|---------|-------|-----|
| `shared_buffers` | 512MB | Larger buffer pool for bulk operations |
| `work_mem` | 64MB | Larger sort/hash memory |
| `maintenance_work_mem` | 256MB | Faster ANALYZE/index builds |
| `max_wal_size` | 2GB | Fewer checkpoints during bulk insert |
| `checkpoint_timeout` | 30min | Defer checkpoints during seeding |
| `shm_size` | 1GB | Shared memory for PG |

### Seeding Strategy

The 25GB dataset is generated using several optimizations:
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
The integration/benchmark `TestMain` auto-detects running containers. If containers from a previous run are still up but in a bad state:
```bash
make test-stop
make test-integration             # fresh start
```

### Port conflicts
Tests expect `:5432` (source) and `:5433` (dest) to be available. Stop any local PostgreSQL instance first:
```bash
# macOS
brew services stop postgresql@18
# Linux
sudo systemctl stop postgresql
```

### Podman users
If using Podman without Docker compatibility:
```bash
export CONTAINER_RUNTIME=podman
make test-integration
```

The Makefile auto-detects `docker compose` → `podman-compose` → `podman compose` in that order.
