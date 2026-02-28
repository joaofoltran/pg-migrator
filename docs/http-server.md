# HTTP Server & API

**Package:** `internal/server`
**Files:** `server.go`, `handlers.go`, `clusters.go`, `jobs.go`, `websocket.go`, `embed.go`

## Overview

The HTTP server provides a unified interface for monitoring and managing pgmanager. It serves four concerns from a single port:

1. **REST API** — JSON endpoints for migration status, per-table progress, configuration, logs, and cluster management
2. **WebSocket** — Real-time push of `Snapshot` updates to connected clients at 500ms intervals
3. **Job Control** — Submit and stop migration pipeline jobs (clone, follow, switchover) via the daemon
4. **Static Files** — Embedded React frontend served via `go:embed` with SPA fallback for client-side routing

The server has zero impact on migration performance — all data is read from the thread-safe `metrics.Collector` with no blocking writes.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    HTTP Server (:7654)                     │
│                                                           │
│  Monitoring:                                              │
│    GET  /api/v1/status     ──► handlers.status()          │
│    GET  /api/v1/tables     ──► handlers.tables()          │
│    GET  /api/v1/config     ──► handlers.configHandler()   │
│    GET  /api/v1/logs       ──► handlers.logs()            │
│    WS   /api/v1/ws         ──► Hub.handleWS()             │
│                                                           │
│  Cluster Management:                                      │
│    GET  /api/v1/clusters       ──► clusterHandlers.list() │
│    POST /api/v1/clusters       ──► clusterHandlers.add()  │
│    GET  /api/v1/clusters/{id}  ──► clusterHandlers.get()  │
│    PUT  /api/v1/clusters/{id}  ──► clusterHandlers.update()│
│    DELETE /api/v1/clusters/{id}──► clusterHandlers.remove()│
│    POST /api/v1/clusters/test-connection                  │
│                                                           │
│  Job Control (daemon mode):                               │
│    POST /api/v1/jobs/clone       ──► submitClone          │
│    POST /api/v1/jobs/follow      ──► submitFollow         │
│    POST /api/v1/jobs/switchover  ──► submitSwitchover     │
│    POST /api/v1/jobs/stop        ──► stopJob              │
│    GET  /api/v1/jobs/status      ──► jobStatus            │
│                                                           │
│  Frontend:                                                │
│    GET  / ──► spaHandler(distFS) with index.html fallback │
│                                                           │
│  Hub ◄── Collector.Subscribe()                            │
│   └── broadcast(snap) ──► [WS Clients]                    │
└──────────────────────────────────────────────────────────┘
```

## Server (`server.go`)

### Construction

```go
srv := server.New(collector, cfg, logger)
srv.SetJobManager(jobs)       // enables job control routes
srv.SetClusterStore(clusters) // enables cluster management routes
```

The server conditionally registers route groups based on which components are attached:
- **Always registered:** status, tables, config, logs, WebSocket, embedded frontend
- **If `JobManager` set:** clone/follow/switchover/stop/status job routes
- **If `ClusterStore` set:** cluster CRUD and connection testing routes

### SPA Fallback

The `spaHandler` wraps the static file server to support React Router's client-side routing. For paths that don't match a static file (e.g. `/clusters`, `/migration`, `/settings`), it serves `index.html` instead of returning 404:

```go
func spaHandler(fsys http.FileSystem) http.Handler {
    fileServer := http.FileServer(fsys)
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        if path != "/" && !strings.HasPrefix(path, "/api/") {
            f, err := fsys.Open(path)
            if err != nil {
                r.URL.Path = "/"
                fileServer.ServeHTTP(w, r)
                return
            }
            f.Close()
        }
        fileServer.ServeHTTP(w, r)
    })
}
```

### Starting

**Blocking mode** (used by `daemon start --foreground`):
```go
err := srv.Start(ctx, 7654)
```

**Background mode** (used by foreground pipeline runs):
```go
srv.StartBackground(ctx, 7654)
```

### Context Propagation

The server's `BaseContext` is set to the caller's context, ensuring all request handlers inherit the daemon's lifecycle.

## REST API — Monitoring (`handlers.go`)

All REST endpoints return JSON with `Content-Type: application/json` and `Access-Control-Allow-Origin: *`.

### `GET /api/v1/status`

Returns the current `Snapshot` as JSON. See [Metrics Collector](metrics-collector.md) for the full Snapshot schema.

### `GET /api/v1/tables`

Returns only the `tables` array from the current snapshot.

### `GET /api/v1/config`

Returns the migration configuration with **passwords redacted**. The `redactDB()` function strips the `Password` field from both source and destination database configs.

### `GET /api/v1/logs`

Returns the log ring buffer contents as a JSON array.

## REST API — Cluster Management (`clusters.go`)

### `GET /api/v1/clusters`

Returns all registered clusters as a JSON array.

### `POST /api/v1/clusters`

Register a new cluster. Validates required fields (id, name, at least one node with host and port).

**Request body:**
```json
{
  "id": "prod",
  "name": "Production",
  "nodes": [
    {"id": "primary", "host": "10.0.0.1", "port": 5432, "role": "primary"},
    {"id": "replica", "host": "10.0.0.2", "port": 5432, "role": "replica"}
  ],
  "tags": ["env:prod"]
}
```

**Response:** `201 Created` with the created cluster (including `created_at`, `updated_at`).

### `GET /api/v1/clusters/{id}`

Returns a single cluster by ID. `404` if not found.

### `PUT /api/v1/clusters/{id}`

Update a cluster. Preserves `created_at`, updates `updated_at`.

### `DELETE /api/v1/clusters/{id}`

Remove a cluster. `204 No Content` on success.

### `POST /api/v1/clusters/test-connection`

Test connectivity to a PostgreSQL instance. Returns reachability, version, replica status, latency, and privilege levels.

**Request body:**
```json
{"dsn": "postgres://user:pass@10.0.0.1:5432/postgres"}
```

**Response:**
```json
{
  "reachable": true,
  "version": "PostgreSQL 16.2",
  "is_replica": false,
  "privileges": {
    "connect": true,
    "replication": true,
    "createdb": false,
    "superuser": false
  },
  "latency_ns": 2300000,
  "error": ""
}
```

## REST API — Job Control (`jobs.go`)

### `POST /api/v1/jobs/clone`

Submit a clone job. See [daemon.ClonePayload](../internal/daemon/daemon.go) for fields.

### `POST /api/v1/jobs/follow`

Submit a follow (CDC-only) job.

### `POST /api/v1/jobs/switchover`

Submit a switchover job with optional timeout.

### `POST /api/v1/jobs/stop`

Stop the currently running job.

### `GET /api/v1/jobs/status`

Returns `{"running": true/false, "last_error": "..."}`.

## WebSocket Hub (`websocket.go`)

Uses `github.com/coder/websocket`. Broadcasts `Snapshot` JSON every 500ms to all connected clients. Each write has a 5-second timeout. Failed writes trigger automatic client removal.

### Client Connection Flow

1. `websocket.Accept()` upgrades the HTTP connection
2. Client added to hub, initial snapshot sent immediately
3. Read loop blocks to keep connection alive
4. On disconnect, client removed from hub

## Embedded Frontend (`embed.go`)

```go
//go:embed all:dist
var distFS embed.FS
```

The `dist/` directory contains the built React frontend. The server extracts a sub-filesystem and serves it via `spaHandler` for client-side routing support.

## Security Considerations

- **Password redaction:** The `/api/v1/config` endpoint strips database passwords
- **Cluster store permissions:** `clusters.json` is written with `0600` permissions
- **Connection test DSN:** DSNs are used transiently for testing, never persisted
- **CORS:** `Access-Control-Allow-Origin: *` is set for development. Restrict in production
- **No authentication:** The API is unauthenticated by design. Bind to localhost or use network-level access control
