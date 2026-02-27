# HTTP Server & API

**Package:** `internal/server`
**Files:** `server.go`, `handlers.go`, `websocket.go`, `embed.go`

## Overview

The HTTP server provides a unified interface for monitoring pgmigrator. It serves three concerns from a single port:

1. **REST API** — JSON endpoints for polling migration status, per-table progress, configuration, and logs
2. **WebSocket** — Real-time push of `Snapshot` updates to connected clients at 500ms intervals
3. **Static Files** — Embedded React frontend served via `go:embed` at the root path

The server is designed to run as a background goroutine within the pipeline process. It has zero impact on migration performance — all data is read from the thread-safe `metrics.Collector` with no blocking writes.

## Architecture

```
┌──────────────────────────────────────────────────┐
│                HTTP Server (:7654)                 │
│                                                    │
│  GET /api/v1/status  ──► handlers.status()         │
│  GET /api/v1/tables  ──► handlers.tables()         │
│  GET /api/v1/config  ──► handlers.configHandler()  │
│  GET /api/v1/logs    ──► handlers.logs()           │
│  WS  /api/v1/ws      ──► Hub.handleWS()           │
│  GET /                ──► FileServer(distFS)       │
│                                                    │
│  Hub ◄── Collector.Subscribe()                     │
│   └── broadcast(snap) ──► [WS Client 1]           │
│                        ──► [WS Client 2]           │
│                        ──► [WS Client N]           │
└──────────────────────────────────────────────────┘
```

## Server (`server.go`)

### Construction

```go
srv := server.New(collector, cfg, logger)
```

Creates a server with:
- A reference to the `metrics.Collector` for data access
- A reference to the `config.Config` for the `/api/v1/config` endpoint
- A `zerolog.Logger` tagged with component `http-server`
- A new WebSocket `Hub` instance

### Starting

**Blocking mode** (used by `pgmigrator serve`):

```go
err := srv.Start(ctx, 7654)
```

- Registers all HTTP routes on a `http.ServeMux`
- Starts the WebSocket hub goroutine
- Starts `ListenAndServe` in a goroutine
- Blocks until `ctx` is cancelled or a listen error occurs
- On context cancellation, calls `srv.Close()` for graceful shutdown

**Background mode** (used by `clone --api-port` and `follow --api-port`):

```go
srv.StartBackground(ctx, 7654)
```

Wraps `Start()` in a goroutine. Any errors are logged but don't propagate (the migration pipeline continues regardless of API server health).

### Route Registration

Uses Go 1.22+ pattern-based routing:

```go
mux.HandleFunc("GET /api/v1/status", h.status)
mux.HandleFunc("GET /api/v1/tables", h.tables)
mux.HandleFunc("GET /api/v1/config", h.configHandler)
mux.HandleFunc("GET /api/v1/logs", h.logs)
mux.HandleFunc("/api/v1/ws", s.hub.handleWS)
mux.Handle("/", http.FileServer(http.FS(sub)))
```

The WebSocket endpoint uses a method-agnostic pattern because the WebSocket upgrade is handled at the protocol level, not the HTTP method level.

### Context Propagation

The server's `BaseContext` is set to the caller's context, ensuring all request handlers inherit the pipeline's lifecycle. When the pipeline shuts down, all in-flight requests are cancelled.

## REST API (`handlers.go`)

All REST endpoints return JSON with `Content-Type: application/json` and `Access-Control-Allow-Origin: *` (for cross-origin development).

### `GET /api/v1/status`

Returns the current `Snapshot` as JSON.

**Response body:**
```json
{
  "timestamp": "2026-02-27T14:23:01.234Z",
  "phase": "streaming",
  "elapsed_sec": 5025.7,
  "applied_lsn": "0/1A3B4C5",
  "confirmed_lsn": "0/1A3B4C0",
  "lag_bytes": 1258291,
  "lag_formatted": "1.20 MB (latency: 0s)",
  "tables_total": 54,
  "tables_copied": 42,
  "tables": [...],
  "rows_per_sec": 4521.3,
  "bytes_per_sec": 1048576.0,
  "total_rows": 22670800,
  "total_bytes": 5368709120,
  "error_count": 0
}
```

### `GET /api/v1/tables`

Returns only the `tables` array from the current snapshot. Useful for lightweight polling of per-table progress.

**Response body:**
```json
[
  {
    "schema": "public",
    "name": "users",
    "status": "copied",
    "rows_total": 1200000,
    "rows_copied": 1200000,
    "size_bytes": 268435456,
    "bytes_copied": 268435456,
    "percent": 100,
    "elapsed_sec": 45.2
  },
  ...
]
```

### `GET /api/v1/config`

Returns the migration configuration with **passwords redacted**. The `redactDB()` function strips the `Password` field from both source and destination database configs, exposing only `host`, `port`, `user`, and `dbname`.

**Response body:**
```json
{
  "source": { "host": "source-pg", "port": 5432, "user": "postgres", "dbname": "mydb" },
  "dest": { "host": "dest-pg", "port": 5432, "user": "postgres", "dbname": "mydb" },
  "replication": { "SlotName": "pgmigrator", "Publication": "pgmigrator_pub", ... },
  "snapshot": { "Workers": 4 }
}
```

Returns `{"error": "no config available"}` if the config reference is nil.

### `GET /api/v1/logs`

Returns the log ring buffer contents as a JSON array.

**Response body:**
```json
[
  {
    "time": "2026-02-27T14:23:01.234Z",
    "level": "info",
    "message": "COPY complete table=public.users rows=1200000"
  },
  ...
]
```

### Error Handling

All handlers use the `writeJSON` helper which:
- Sets proper content type and CORS headers
- Uses `json.NewEncoder` for streaming serialization (no intermediate buffer)
- Returns HTTP 500 with the error message if JSON encoding fails

## WebSocket Hub (`websocket.go`)

### Purpose

The WebSocket hub provides real-time push updates to browser clients and remote monitoring tools. Instead of polling the REST API, clients connect once and receive `Snapshot` JSON messages every 500ms automatically.

### Library

Uses `github.com/coder/websocket` (the maintained fork of `nhooyr.io/websocket`). This library is context-aware, supports modern compression, and handles ping/pong automatically.

### Hub Lifecycle

```go
hub := newHub(collector, logger)
go hub.start(ctx)
```

1. `newHub()` creates the hub with an empty client set
2. `start()` subscribes to the collector and enters a broadcast loop
3. On each received snapshot, `broadcast()` serializes to JSON once and writes to all clients
4. When `ctx` is cancelled, the hub unsubscribes and exits

### Client Connection Flow

When a WebSocket upgrade request arrives at `/api/v1/ws`:

1. `websocket.Accept()` upgrades the HTTP connection
   - `InsecureSkipVerify: true` allows cross-origin connections (required for Vite dev proxy)
2. The client is added to the hub's client map
3. An initial snapshot is sent immediately (so the client doesn't have to wait 500ms)
4. The handler enters a read loop, blocking on `conn.Read()` to keep the connection alive
5. When the read returns an error (client disconnect), the client is removed from the hub

### Broadcasting

```go
func (h *Hub) broadcast(snap metrics.Snapshot) {
    data, _ := json.Marshal(snap)  // Serialize once
    for _, client := range clients {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        err := client.conn.Write(ctx, websocket.MessageText, data)
        cancel()
        if err != nil {
            h.remove(client)  // Dead client, clean up
        }
    }
}
```

- JSON serialization happens once per broadcast cycle, not per client
- Each write has a 5-second timeout to prevent a slow client from blocking others
- Failed writes trigger automatic client removal and connection close
- The client list is copied under lock before iteration to minimize lock duration

### Concurrency Safety

- `h.mu` (sync.Mutex) protects the clients map
- Lock is held briefly during add/remove/copy operations, never during I/O
- The `done` channel on `wsClient` is available for future graceful shutdown signaling

## Embedded Frontend (`embed.go`)

```go
//go:embed all:dist
var distFS embed.FS
```

The `all:` prefix includes files starting with `.` or `_`. The `dist/` directory contains:
- Built React frontend when `make web-build` has been run
- A fallback `index.html` placeholder otherwise

The server extracts a sub-filesystem rooted at `dist/`:

```go
sub, _ := fs.Sub(distFS, "dist")
mux.Handle("/", http.FileServer(http.FS(sub)))
```

This means the React app's `index.html`, JavaScript bundles, and CSS files are all served from the Go binary with zero external dependencies.

## Usage Patterns

### Embedded in Pipeline (Most Common)

```bash
pgmigrator clone --follow --api-port=7654
```

The pipeline starts the server as a background goroutine. The API reflects live data from the in-process `metrics.Collector`.

### Standalone Web Server

```bash
pgmigrator serve --port=7654
```

Starts only the HTTP server. Creates a fresh collector and attempts to load the last state from `~/.pgmigrator/state.json`. Useful for viewing the last-known state of a completed or crashed migration.

### Remote TUI

```bash
pgmigrator tui --api-addr=http://prod-server:7654
```

The TUI polls `GET /api/v1/status` every 500ms and feeds the data into a local collector for rendering. The WebSocket endpoint is not used by the TUI (it uses simple HTTP polling for simplicity and reliability).

## Security Considerations

- **Password redaction:** The `/api/v1/config` endpoint strips database passwords before serialization
- **CORS:** `Access-Control-Allow-Origin: *` is set for development convenience. In production, consider restricting this
- **No authentication:** The API is unauthenticated by design (it's a local monitoring tool). Bind to localhost or use network-level access control for production
- **WebSocket origin:** `InsecureSkipVerify: true` allows any origin to connect. This is intentional for dev workflows where the Vite dev server runs on a different port
