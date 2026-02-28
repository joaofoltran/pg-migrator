# Development

How to build, run, and iterate on pgmanager locally.

## Requirements

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.24+ | Backend |
| Node | 22+ | Frontend build (or bun) |
| Docker or Podman | any | Backend PostgreSQL, container builds |

## Project Structure

```
cm./pgmanager/         Single-file entrypoint (main.go)
internal/
  appconfig/          TOML config loader + env var overrides
  db/                 PG connection pool + embedded schema migrations
  db/migrations/      SQL migration files (applied in filename order)
  cluster/            PostgreSQL-backed cluster store
  server/             HTTP server, API handlers, embedded SPA
  migration/          Migration engine (stream, replay, snapshot, schema, etc.)
  metrics/            Metrics collector + state
  daemon/             Legacy daemon code (not wired, pending removal)
web/                  React/TypeScript SPA (Vite + Tailwind)
config.example.toml   Reference config file
Dockerfile            Multi-stage production build
docker-compose.yml    App + backend PG for container deployment
```

## Quick Start (Containers)

The fastest way to get everything running:

```bash
# Build and start (pgmanager + backend PostgreSQL)
docker compose up --build -d

# Verify
docker compose logs pgmanager    # should show "starting pgmanager" on :7654
open http://localhost:7654       # web UI
```

To rebuild after code changes:

```bash
docker compose up --build -d    # rebuilds only changed layers
docker compose logs -f pgmanager # watch logs
```

Tear down:

```bash
docker compose down       # keeps database volume
docker compose down -v    # also deletes database volume
```

## Quick Start (Native)

Run the backend and frontend separately for faster iteration.

### 1. Start a PostgreSQL instance

Use the backend database from compose:

```bash
docker compose up db -d --wait
```

Or any local PostgreSQL — just have a database ready.

### 2. Build and run the Go backend

```bash
# Build frontend assets into internal/server/dist/
make web-build

# Build the binary
make build

# Run with env vars (or create ~/.pgmanager/config.toml from config.example.toml)
PGMANAGER_DB_URL="postgres:./pgmanager:migrator@localhost:543./pgmanager?sslmode=disable" \
PGMANAGER_LOG_LEVEL=debug \
  ./pgmanager
```

The server starts on `http://localhost:7654` by default.

### 3. Frontend dev server (hot reload)

For frontend work, run Vite's dev server alongside the Go backend:

```bash
# Terminal 1: Go backend (serves API on :7654)
PGMANAGER_DB_URL="postgres:./pgmanager:migrator@localhost:543./pgmanager?sslmode=disable" ./pgmanager

# Terminal 2: Vite dev server (serves UI on :5173, proxies API to :7654)
cd web && npm run dev
```

Open `http://localhost:5173` — Vite proxies `/api/*` requests to the Go backend.

## Configuration

pgmanager loads config from (in priority order):

1. `--config <path>` flag
2. `~/.pgmanager/config.toml`
3. `/etc/pgmanager/config.toml`
4. Environment variables (always win over file values)
5. Built-in defaults

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PGMANAGER_LISTEN` | `127.0.0.1` | Bind address |
| `PGMANAGER_PORT` | `7654` | HTTP port |
| `PGMANAGER_DB_URL` | `postgres://localhost:543./pgmanager?sslmode=disable` | Backend PostgreSQL URL |
| `PGMANAGER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PGMANAGER_LOG_FORMAT` | `console` | `console` (human) or `json` (structured) |

### Example Config File

```toml
[server]
listen = "127.0.0.1"
port = 7654

[database]
url = "postgres:./pgmanager:migrator@localhost:543./pgmanager?sslmode=disable"

[logging]
level = "info"
format = "console"
```

See `config.example.toml` for a copy-pasteable version.

## Build Targets

```bash
make build          # Go binary only (requires pre-built frontend in internal/server/dist/)
make web-build      # Frontend: npm ci + vite build + copy to internal/server/dist/
make build-full     # Frontend + Go binary (full production build)
make docker         # Docker image build
make lint           # go vet ./...
make clean          # rm pgmanager binary
make install        # build-full + copy to /usr/local/bin/
```

## Docker Build

The `Dockerfile` is a three-stage build:

| Stage | Base | Purpose |
|-------|------|---------|
| `frontend` | `node:22-alpine` | `npm install` + `vite build` |
| `backend` | `golang:1.24-alpine` | `go mod download` + `go build` (CGO_ENABLED=0, stripped) |
| runtime | `alpine:3.21` | Final image with `ca-certificates` + `postgresql-client` |

```bash
# Build image
docker build -t pgmanager .

# Run standalone (needs a PG instance)
docker run -p 7654:7654 \
  -e PGMANAGER_DB_URL="postgres:./pgmanager:migrator@host.docker.internal:543./pgmanager?sslmode=disable" \
  -e PGMANAGER_LISTEN="0.0.0.0" \
  pgmanager
```

The `.dockerignore` excludes `.git`, `web/node_modules`, `web/dist`, markdown files, logs, and the local binary.

## Database Migrations

Schema migrations live in `internal/db/migrations/*.sql` and are embedded in the binary via `//go:embed`. They run automatically on startup — no manual migration step needed.

Naming convention: `NNN_description.sql` (e.g., `001_clusters.sql`). Files are applied in sorted filename order. Each migration runs inside a transaction. Applied migrations are tracked in the `schema_migrations` table.

To add a new migration:

```bash
touch internal/db/migrations/002_your_feature.sql
# Write your SQL, then rebuild
make build
```

## API Endpoints

All routes are under `/api/v1/`. The SPA is served at `/`.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/clusters` | List all clusters |
| POST | `/api/v1/clusters` | Create a cluster |
| GET | `/api/v1/clusters/{id}` | Get a cluster |
| PUT | `/api/v1/clusters/{id}` | Update a cluster |
| DELETE | `/api/v1/clusters/{id}` | Delete a cluster |
| POST | `/api/v1/clusters/test-connection` | Test a DSN connection |
| GET | `/api/v1/clusters/{id}/introspect` | Introspect a cluster node |

## Podman Notes

pgmanager fully supports Podman as an alternative to Docker. The Makefile and test harness auto-detect the runtime in this order: `docker compose` → `podman-compose` → `podman compose`.

### Quick Start with Podman

```bash
# All compose commands work the same as Docker
podman compose up --build -d
podman compose logs pgmanager
podman compose down -v
```

### Rootless Podman Setup

Podman runs rootless by default, which is more secure but requires a few extra steps:

```bash
# Ensure user namespaces are configured
podman system migrate

# Verify rootless mode works
podman run --rm alpine echo "rootless OK"
```

### Podman Socket (for tools expecting Docker socket)

Some tools expect a Docker-compatible socket. Enable the Podman socket:

```bash
# macOS (Podman Machine)
podman machine init   # first time only
podman machine start

# Linux (systemd user service)
systemctl --user enable --now podman.socket

# Verify socket
podman info --format '{{.Host.RemoteSocket.Path}}'
```

### Compose Options

Podman supports two compose implementations:

```bash
# Option 1: Built-in (Podman 4.7+)
podman compose up --build -d

# Option 2: podman-compose (pip install podman-compose)
podman-compose up --build -d

# Option 3: docker-compose with Podman socket
export DOCKER_HOST="unix://$(podman info --format '{{.Host.RemoteSocket.Path}}')"
docker-compose up --build -d
```

### Host Networking Differences

When connecting from a container to the host (e.g., to a local PostgreSQL):

| Runtime | Host address |
|---------|-------------|
| Docker | `host.docker.internal` |
| Podman (macOS) | `host.containers.internal` |
| Podman (Linux rootless) | `host.containers.internal` or `10.0.2.2` |

Example — running pgmanager in a container against a host-local PG:

```bash
podman run -p 7654:7654 \
  -e PGMANAGER_DB_URL="postgres:./pgmanager:migrator@host.containers.internal:543./pgmanager?sslmode=disable" \
  -e PGMANAGER_LISTEN="0.0.0.0" \
  pgmanager
```

### Running Tests with Podman

```bash
# Override runtime explicitly if auto-detect doesn't work
export CONTAINER_RUNTIME=podman

# Integration tests
make test-integration

# Benchmarks
make test-benchmark

# Tear down
make test-stop
```

### Building the Image

```bash
podman build -t pgmanager .
podman images pgmanager
```

### Troubleshooting Podman

| Issue | Fix |
|-------|-----|
| `--wait` flag not supported | Upgrade Podman to 4.7+ or use `podman compose up -d` (test harness auto-retries without `--wait`) |
| Port conflicts | Check `podman ps -a` for orphaned containers |
| Volume permission denied | Run `podman unshare chown` or set `:Z` SELinux label on bind mounts |
| Slow image builds | Enable `podman machine` with more CPUs/memory: `podman machine set --cpus 4 --memory 4096` |
| `docker-compose` not finding Podman | Set `DOCKER_HOST` to Podman socket path (see above) |

## Typical Dev Workflows

### Backend change (Go)

```bash
make build && ./pgmanager     # rebuild + restart
# or with live PG:
PGMANAGER_DB_URL="..." ./pgmanager
```

### Frontend change (React/TS)

```bash
cd web && npm run dev        # hot reload on :5173
```

### Full rebuild after pulling changes

```bash
make build-full              # frontend + backend
# or in containers:
docker compose up --build -d
```

### Adding a new API endpoint

1. Add handler in `internal/server/`
2. Register route in `server.go`
3. Run `go build ./...` to verify compilation
4. Run `make test` for unit tests
5. Test with curl: `curl http://localhost:7654/api/v1/your-endpoint`

### Adding a new database migration

1. Create `internal/db/migrations/NNN_description.sql`
2. Rebuild and restart — migration runs automatically
3. Verify: `docker compose exec db psql -U migrator -c "SELECT * FROM schema_migrations;"`
