# Installer, Docker & Build

**Files:** `scripts/install.sh`, `Dockerfile`, `docker-compose.yml`, `Makefile`

## Overview

pgmigrator provides multiple installation and deployment methods: a shell installer script for downloading pre-built binaries from GitHub releases, a multi-stage Dockerfile for containerized deployment, a docker-compose file for quick development/testing with source and destination databases, and Makefile targets for local development including frontend builds.

---

## Shell Installer (`scripts/install.sh`)

### Usage

```bash
# Install latest version
curl -sSL https://raw.githubusercontent.com/jfoltran/pgmigrator/main/scripts/install.sh | bash

# Install specific version
PGMIGRATOR_VERSION=v1.0.0 bash scripts/install.sh
```

### What It Does

1. **Detects OS** — `uname -s` → `linux` or `darwin`
2. **Detects architecture** — `uname -m` → `amd64` or `arm64`
3. **Gets latest version** — Queries GitHub API for the latest release tag
4. **Downloads binary** — Fetches the release tarball from GitHub
5. **Verifies checksum** — Downloads `checksums.txt` and verifies SHA256
6. **Installs** — Moves binary to `/usr/local/bin/pgmigrator`
7. **Verifies** — Runs `pgmigrator --help` to confirm the binary works

### Platform Detection

| `uname -s` | OS |
|-------------|-----|
| `Linux*` | `linux` |
| `Darwin*` | `darwin` |
| Other | Error: "Unsupported OS" |

| `uname -m` | Architecture |
|-------------|--------------|
| `x86_64`, `amd64` | `amd64` |
| `aarch64`, `arm64` | `arm64` |
| Other | Error: "Unsupported architecture" |

### Version Resolution

The version is determined by (in priority order):

1. `PGMIGRATOR_VERSION` environment variable (if set)
2. Latest release from GitHub API: `GET https://api.github.com/repos/jfoltran/pgmigrator/releases/latest`

The GitHub API response is parsed with `grep` and `sed` to extract the `tag_name` field. This avoids requiring `jq` as a dependency.

### Download

The expected asset name follows the pattern:
```
pgmigrator_{version}_{os}_{arch}.tar.gz
```

For example: `pgmigrator_1.0.0_linux_amd64.tar.gz`

The script supports both `curl` and `wget` — it uses whichever is available. If neither is found, it exits with an error.

### SHA256 Verification

The script downloads `checksums.txt` from the same release. If available:

1. Extracts the expected checksum for the downloaded asset
2. Computes the actual checksum using `sha256sum` (Linux) or `shasum -a 256` (macOS)
3. Compares them — if they don't match, aborts with "Checksum mismatch!"
4. If checksums aren't available, prints a warning and continues

### Installation

The script checks if `/usr/local/bin` is writable:
- **Writable:** Moves the binary directly
- **Not writable:** Uses `sudo` to move the binary

After installation, sets the executable permission (`chmod +x`) and runs `pgmigrator --help | head -3` to verify the binary works.

### Safety Features

| Feature | Implementation |
|---------|---------------|
| `set -euo pipefail` | Exit on error, undefined variable, or pipe failure |
| Temp directory cleanup | `trap 'rm -rf "$tmp_dir"' EXIT` |
| Checksum verification | SHA256 check before installation |
| No blind `sudo` | Only uses sudo when the install directory isn't writable |
| Color-coded output | `[INFO]`, `[WARN]`, `[ERROR]` with ANSI colors |

---

## Dockerfile

### Multi-Stage Build

The Dockerfile uses three stages to minimize the final image size:

#### Stage 1: Frontend Build (`node:20-alpine`)

```dockerfile
FROM node:20-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ .
RUN npm run build
```

- Copies `package.json` first for Docker layer caching (dependencies change less often than source code)
- `npm ci` for deterministic, clean installs
- The `package-lock.json*` glob handles the case where the lock file doesn't exist yet
- `npm run build` produces the optimized React frontend in `dist/`

#### Stage 2: Go Build (`golang:1.23-alpine`)

```dockerfile
FROM golang:1.23-alpine AS backend
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist internal/server/dist/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o pgmigrator ./cmd/pgmigrator
```

- `git` is needed for Go modules that reference git repositories
- Copies `go.mod`/`go.sum` first for dependency layer caching
- `go mod download` pre-downloads all dependencies
- Copies the frontend build output into `internal/server/dist/` for `go:embed`
- `CGO_ENABLED=0` produces a statically-linked binary (no libc dependency)
- `-ldflags="-s -w"` strips debug symbols and DWARF info, reducing binary size by ~30%

#### Stage 3: Runtime (`alpine:3.19`)

```dockerfile
FROM alpine:3.19
RUN apk add --no-cache ca-certificates postgresql-client
COPY --from=backend /app/pgmigrator /usr/local/bin/pgmigrator
EXPOSE 7654
ENTRYPOINT ["pgmigrator"]
```

- **`ca-certificates`** — Required for HTTPS connections (e.g., if PostgreSQL uses SSL)
- **`postgresql-client`** — Provides `pg_dump`, required by the schema package's `DumpSchema()` method
- Exposes port 7654 for the Web UI/API server
- `ENTRYPOINT` allows passing subcommands directly: `docker run pgmigrator clone --follow`

### Image Size

| Stage | Base Image | Approximate Size |
|-------|-----------|------------------|
| Frontend | node:20-alpine | ~180 MB (not in final) |
| Backend | golang:1.23-alpine | ~350 MB (not in final) |
| Runtime | alpine:3.19 | ~15 MB + pgmigrator binary (~15-20 MB) + postgresql-client (~10 MB) |

Final image: approximately **45-50 MB**.

### Build Command

```bash
docker build -t pgmigrator .
# or
make docker
```

### Run Examples

```bash
# Clone with streaming
docker run --rm pgmigrator clone --follow \
    --source-host=10.0.0.1 --source-dbname=prod \
    --dest-host=10.0.0.2 --dest-dbname=staging

# With web UI
docker run --rm -p 7654:7654 pgmigrator clone --follow --api-port=7654 \
    --source-host=10.0.0.1 --source-dbname=prod \
    --dest-host=10.0.0.2 --dest-dbname=staging
```

---

## Docker Compose (`docker-compose.yml`)

### Services

A complete development/testing environment with three services:

#### `pgmigrator`

```yaml
pgmigrator:
  build: .
  ports:
    - "7654:7654"
  command:
    - clone
    - --follow
    - --api-port=7654
    - --source-host=source-pg
    - --source-dbname=source
    - --source-user=postgres
    - --source-password=source
    - --dest-host=dest-pg
    - --dest-dbname=dest
    - --dest-user=postgres
    - --dest-password=dest
  depends_on:
    source-pg:
      condition: service_healthy
    dest-pg:
      condition: service_healthy
```

- Builds from the local Dockerfile
- Exposes the Web UI on port 7654
- Runs `clone --follow` with API server enabled
- **Waits for both databases to be healthy** via `depends_on` with `service_healthy` condition

#### `source-pg`

```yaml
source-pg:
  image: postgres:16
  environment:
    POSTGRES_DB: source
    POSTGRES_PASSWORD: source
  command:
    - postgres
    - -c
    - wal_level=logical
    - -c
    - max_replication_slots=4
    - -c
    - max_wal_senders=4
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U postgres"]
    interval: 5s
    timeout: 5s
    retries: 5
```

**Critical configuration:**
- **`wal_level=logical`** — Required for logical replication. Without this, `CREATE_REPLICATION_SLOT` with `pgoutput` plugin will fail
- **`max_replication_slots=4`** — Allows up to 4 replication slots (default is 10, but explicitly set for clarity)
- **`max_wal_senders=4`** — Allows up to 4 concurrent WAL sender processes

The healthcheck uses `pg_isready` to verify PostgreSQL is accepting connections before pgmigrator starts.

#### `dest-pg`

```yaml
dest-pg:
  image: postgres:16
  environment:
    POSTGRES_DB: dest
    POSTGRES_PASSWORD: dest
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U postgres"]
    interval: 5s
    timeout: 5s
    retries: 5
```

The destination doesn't need `wal_level=logical` — it's only receiving writes, not producing a replication stream. (For bidirectional replication, both databases would need `wal_level=logical`.)

### Usage

```bash
# Start everything
docker compose up

# Start in background
docker compose up -d

# View logs
docker compose logs -f pgmigrator

# Open Web UI
open http://localhost:7654

# Tear down
docker compose down -v
```

---

## Makefile

### Targets

| Target | Dependencies | Description |
|--------|-------------|-------------|
| `build` | — | Build Go binary → `./pgmigrator` |
| `test` | — | Run all Go tests with verbose output |
| `lint` | — | Run `go vet` on all packages |
| `clean` | — | Remove the binary |
| `web-install` | — | Install frontend npm dependencies |
| `web-build` | `web-install` | Build frontend, copy to `internal/server/dist/` |
| `web-dev` | — | Start Vite dev server for frontend development |
| `build-full` | `web-build`, `build` | Build frontend then Go binary (production) |
| `docker` | — | Build Docker image tagged `pgmigrator` |
| `install` | `build-full` | Build everything and copy to `/usr/local/bin/` |

### Common Workflows

#### Development (Go only)

```bash
make build    # Fast Go-only build (uses placeholder frontend)
make test     # Run tests
make lint     # Check for issues
```

#### Development (with frontend)

```bash
# Terminal 1: Start Vite dev server with hot reload
make web-dev

# Terminal 2: Build and run Go with API server
make build
./pgmigrator clone --follow --api-port=7654 ...
```

The Vite dev server (port 5173) proxies API requests to `:7654`, so the frontend hot-reloads while communicating with the real backend.

#### Production Build

```bash
make build-full    # Build frontend + embed + Go binary
# or
make install       # Build everything + install to /usr/local/bin/
```

#### Docker Build

```bash
make docker        # Build Docker image
docker compose up  # Run with test databases
```

### Frontend Build Pipeline

The `web-build` target:

1. `cd web && npm ci` — Clean install of npm dependencies
2. `cd web && npm run build` — Vite production build → `web/dist/`
3. `rm -rf internal/server/dist` — Clear old embedded files
4. `mkdir -p internal/server/dist` — Ensure directory exists
5. `cp -r web/dist/* internal/server/dist/` — Copy build output for `go:embed`

After this, `go build` embeds the production React app into the Go binary via `//go:embed all:dist` in `internal/server/embed.go`.

### Variables

```makefile
BINARY := pgmigrator
PKG := ./cmd/pgmigrator
```

All targets reference these variables for consistency.

---

## Deployment Patterns

### Bare Metal / VM

```bash
# Install
curl -sSL https://raw.githubusercontent.com/jfoltran/pgmigrator/main/scripts/install.sh | bash

# Run migration
pgmigrator clone --follow --api-port=7654 \
    --source-host=source.db.internal --source-dbname=production \
    --dest-host=dest.db.internal --dest-dbname=production \
    --source-user=replication_user --source-password="$SOURCE_PW" \
    --dest-user=migration_user --dest-password="$DEST_PW"

# Monitor (separate terminal)
open http://localhost:7654
# or
pgmigrator tui
```

### Docker (Production)

```bash
docker run -d --name pgmigrator \
    -p 7654:7654 \
    --restart unless-stopped \
    pgmigrator clone --follow --api-port=7654 \
    --source-host=source.db.internal --source-dbname=production \
    --dest-host=dest.db.internal --dest-dbname=production
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pgmigrator
spec:
  replicas: 1  # Must be exactly 1 — replication slot is exclusive
  template:
    spec:
      containers:
      - name: pgmigrator
        image: pgmigrator:latest
        args:
          - clone
          - --follow
          - --api-port=7654
        ports:
        - containerPort: 7654
        env:
        - name: SOURCE_PASSWORD
          valueFrom:
            secretKeyRef:
              name: pg-secrets
              key: source-password
```

**Note:** pgmigrator must run as a single replica. Logical replication slots are exclusive — only one consumer can read from a slot at a time.
