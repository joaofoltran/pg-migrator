# Web UI

**Directory:** `web/`
**Tech Stack:** React 18, TypeScript, Vite, Tailwind CSS, Recharts, React Router, lucide-react
**Embedding:** `internal/server/embed.go` via `go:embed`

## Overview

The web UI is a multi-page React application with sidebar navigation for managing PostgreSQL clusters and monitoring migrations. It connects to the daemon's HTTP API via WebSocket for real-time updates and provides cluster registration, migration monitoring, and module switching.

The built frontend is embedded into the Go binary via `go:embed`, so the `migrator` binary is fully self-contained. The Go server includes a SPA fallback handler that serves `index.html` for all client-side routes.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                     Browser                           │
│                                                       │
│  BrowserRouter                                        │
│    └── Layout (flex: Sidebar + Outlet)                │
│          │                                            │
│          ├── Sidebar                                  │
│          │     ├── Logo + branding                    │
│          │     ├── Infrastructure: Clusters            │
│          │     ├── Modules: Migration, Backup, Standby│
│          │     ├── Settings                           │
│          │     └── Daemon status indicator             │
│          │                                            │
│          └── Pages (via React Router)                 │
│                ├── /clusters     → ClustersPage       │
│                ├── /migration    → MigrationPage      │
│                │     └── useMetrics() ◄── WebSocket   │
│                ├── /backup       → BackupPage (soon)  │
│                ├── /standby      → StandbyPage (soon) │
│                └── /settings     → SettingsPage       │
└──────────────────────────────────────────────────────┘
```

## Build & Development

### Development Server

```bash
cd web && bun run dev
```

Starts Vite dev server on `http://localhost:5173` with hot module replacement. API requests are proxied to `http://localhost:7654` (configured in `vite.config.ts`).

### Production Build

```bash
cd web && bun run build
rm -rf internal/server/dist && mkdir -p internal/server/dist && cp -r web/dist/* internal/server/dist/
go build ./...
```

Or via Makefile:

```bash
make build-full   # web-build + go build
```

## Project Structure

```
web/
├── package.json            Dependencies and scripts
├── tsconfig.json           TypeScript config (ES2020, strict)
├── vite.config.ts          Vite config with API proxy
├── tailwind.config.js      Tailwind content paths
├── index.html              HTML entry point
└── src/
    ├── main.tsx            React entry (StrictMode + root render)
    ├── App.tsx             BrowserRouter + Route definitions
    ├── index.css           Tailwind directives + CSS custom properties
    ├── types/
    │   ├── metrics.ts      Snapshot, TableProgress, LogEntry types
    │   └── cluster.ts      Cluster, ClusterNode, ConnTestResult types
    ├── api/
    │   ├── client.ts       REST client (status, tables, logs, clusters, jobs)
    │   └── websocket.ts    WebSocket connection manager
    ├── hooks/
    │   └── useMetrics.ts   React hook for real-time snapshot data
    ├── pages/
    │   ├── ClustersPage.tsx    Cluster registration and management
    │   ├── MigrationPage.tsx   Migration monitoring dashboard
    │   ├── BackupPage.tsx      Placeholder (coming soon)
    │   ├── StandbyPage.tsx     Placeholder (coming soon)
    │   └── SettingsPage.tsx    Daemon configuration display
    └── components/
        ├── layout/
        │   ├── Layout.tsx      Flex container: Sidebar + Outlet
        │   └── Sidebar.tsx     Persistent nav with module switching
        └── migration/
            ├── PhaseHeader.tsx     Phase badge, elapsed time, lag, connection
            ├── MetricCards.tsx     4-card grid: rows/s, throughput, totals
            ├── OverallProgress.tsx Progress bar with table count
            ├── LagChart.tsx       Recharts line chart for replication lag
            ├── LogViewer.tsx      Polling log viewer with level colors
            ├── TableList.tsx      Per-table progress with status badges
            └── JobControls.tsx    Start/stop migration jobs
```

## Design System

CSS custom properties defined in `index.css`:

| Variable | Value | Usage |
|----------|-------|-------|
| `--color-bg` | `#0a0a0f` | Page background |
| `--color-surface` | `#12121a` | Card/panel background |
| `--color-surface-hover` | `#1a1a25` | Hover state |
| `--color-border` | `#1e1e2e` | Card borders |
| `--color-border-subtle` | `#16161f` | Subtle separators |
| `--color-text` | `#e4e4ef` | Primary text |
| `--color-text-secondary` | `#8888a0` | Secondary text |
| `--color-text-muted` | `#55556a` | Muted/disabled text |
| `--color-accent` | `#6366f1` | Active/accent (indigo) |
| `--color-accent-hover` | `#818cf8` | Accent hover state |

Components use `style={{ color: "var(--color-text-muted)" }}` for custom colors alongside Tailwind utility classes for layout.

## Routing

**`App.tsx`** defines the route structure:

| Path | Page | Description |
|------|------|-------------|
| `/` | Redirect → `/clusters` | Default landing page |
| `/clusters` | `ClustersPage` | Cluster registration and management |
| `/migration` | `MigrationPage` | Migration monitoring dashboard |
| `/backup` | `BackupPage` | Placeholder with "Coming soon" badge |
| `/standby` | `StandbyPage` | Placeholder with "Coming soon" badge |
| `/settings` | `SettingsPage` | Daemon port, data directory, version |

All routes render inside the `Layout` component which provides the persistent `Sidebar`.

## Pages

### ClustersPage

The default landing page for cluster management:

- **Empty state** — prompts user to add their first cluster
- **Cluster list** — expandable cards showing cluster name, ID, node count, tags
- **Node details** — role-colored badges (primary=green, replica=blue, standby=yellow), host:port
- **Add cluster form** — inline form with ID, name, host, port, role fields
- **Remove** — per-cluster delete button with confirmation

Uses `fetchClusters()`, `addCluster()`, `removeCluster()` from the API client.

### MigrationPage

Real-time migration monitoring with the `useMetrics()` hook:

- **Loading state** — spinner while connecting to daemon WebSocket
- **Idle state** — shows phase header and job controls only
- **Active state** — full dashboard with metrics, progress, charts, logs, tables
- **Job controls** — dropdown to start clone/follow, stop button when running

Components: `PhaseHeader`, `MetricCards`, `OverallProgress`, `LagChart`, `LogViewer`, `TableList`, `JobControls`

### BackupPage / StandbyPage

Placeholder pages with icon, description, and "Coming soon" badge. Ready for future module implementation.

### SettingsPage

Displays daemon configuration: port, data directory, version.

## Sidebar (`components/layout/Sidebar.tsx`)

Persistent sidebar with two navigation sections:

- **Infrastructure** — Clusters
- **Modules** — Migration, Backup (soon), Standby (soon)
- **Settings** — bottom link
- **Daemon status** — connection indicator at the very bottom

Active state uses `var(--color-accent)` background. "Coming soon" modules are disabled with 50% opacity.

## API Layer (`api/client.ts`)

### Migration

- `fetchStatus()` — `GET /api/v1/status` → `Snapshot`
- `fetchTables()` — `GET /api/v1/tables` → `TableProgress[]`
- `fetchLogs()` — `GET /api/v1/logs` → `LogEntry[]`
- `submitClone()` — `POST /api/v1/jobs/clone`
- `submitFollow()` — `POST /api/v1/jobs/follow`
- `stopJob()` — `POST /api/v1/jobs/stop`

### Clusters

- `fetchClusters()` — `GET /api/v1/clusters` → `Cluster[]`
- `fetchCluster(id)` — `GET /api/v1/clusters/{id}` → `Cluster`
- `addCluster(data)` — `POST /api/v1/clusters` → `Cluster`
- `updateCluster(id, data)` — `PUT /api/v1/clusters/{id}` → `Cluster`
- `removeCluster(id)` — `DELETE /api/v1/clusters/{id}`
- `testConnection(dsn)` — `POST /api/v1/clusters/test-connection` → `ConnTestResult`

## WebSocket Connection (`api/websocket.ts`)

The `WSConnection` class manages a persistent WebSocket with exponential backoff reconnection:

- Initial delay: 1 second, doubles on each failure, max 30 seconds
- Resets to 1 second on successful connection
- `close()` prevents further reconnection attempts
- Auto-detects `ws:` vs `wss:` from `window.location.protocol`

## Dependencies

### Runtime

| Package | Purpose |
|---------|---------|
| `react` + `react-dom` | UI framework |
| `react-router-dom` | Client-side routing |
| `recharts` | Line chart for replication lag |
| `lucide-react` | Icon library |

### Development

| Package | Purpose |
|---------|---------|
| `typescript` | Type checking |
| `vite` + `@vitejs/plugin-react` | Build tool and dev server |
| `tailwindcss` + `postcss` + `autoprefixer` | Utility CSS |
| `@types/react` + `@types/react-dom` + `@types/react-router-dom` | Type definitions |
