# Web UI

**Directory:** `web/`
**Tech Stack:** React 18, TypeScript, Vite, Tailwind CSS, Recharts
**Embedding:** `internal/server/embed.go` via `go:embed`

## Overview

The web UI is a single-page React application that provides a browser-based dashboard for monitoring pgmigrator migrations. It connects to the pipeline's HTTP API via WebSocket for real-time updates and renders charts, progress bars, and table lists with automatic refresh.

The built frontend is embedded into the Go binary via `go:embed`, so the `pgmigrator` binary is fully self-contained — no external web server or file serving needed.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   Browser                        │
│                                                  │
│  App ──► useMetrics() hook                       │
│              │                                   │
│              ├── WSConnection ◄─── /api/v1/ws    │
│              │       │                           │
│              │       ▼                           │
│              ├── snapshot state                   │
│              ├── history[] (last 300 snapshots)   │
│              │                                   │
│              ▼                                   │
│          Dashboard                               │
│           ├── ThroughputCard                     │
│           ├── ProgressBar                        │
│           ├── LagChart (Recharts)                │
│           ├── LogViewer (polls /api/v1/logs)     │
│           └── TableProgress                      │
└─────────────────────────────────────────────────┘
```

## Build & Development

### Development Server

```bash
cd web && npm run dev
```

Starts Vite dev server on `http://localhost:5173` with hot module replacement. API requests are proxied to `http://localhost:7654` (configured in `vite.config.ts`).

### Production Build

```bash
make web-build
```

This runs:
1. `cd web && npm ci` — Install dependencies
2. `cd web && npm run build` — TypeScript compilation + Vite production build
3. Copies `web/dist/*` to `internal/server/dist/`

The Go binary then embeds `internal/server/dist/` via `go:embed`.

### Full Build

```bash
make build-full   # web-build + go build
```

## Project Structure

```
web/
├── package.json           # Dependencies and scripts
├── tsconfig.json          # TypeScript config (ES2020, strict mode)
├── vite.config.ts         # Vite config with API proxy
├── tailwind.config.js     # Tailwind content paths
├── postcss.config.js      # PostCSS with Tailwind + Autoprefixer
├── index.html             # HTML entry point
└── src/
    ├── main.tsx           # React entry (StrictMode + root render)
    ├── App.tsx            # Root component with loading state
    ├── index.css          # Tailwind directives
    ├── types/
    │   └── metrics.ts     # TypeScript types matching Go structs
    ├── api/
    │   ├── client.ts      # REST client functions
    │   └── websocket.ts   # WebSocket connection manager
    ├── hooks/
    │   └── useMetrics.ts  # React hook for real-time data
    └── components/
        ├── Dashboard.tsx      # Main layout
        ├── PhaseIndicator.tsx # Phase badge + elapsed
        ├── ProgressBar.tsx    # Overall progress
        ├── ThroughputCard.tsx # Metric cards grid
        ├── TableProgress.tsx  # Per-table table
        ├── LagChart.tsx       # Lag line chart
        └── LogViewer.tsx      # Scrollable log tail
```

## TypeScript Types (`types/metrics.ts`)

These types mirror the Go `Snapshot` and `TableProgress` structs exactly, using snake_case to match the JSON serialization:

```typescript
interface Snapshot {
  timestamp: string;
  phase: string;
  elapsed_sec: number;
  applied_lsn: string;
  confirmed_lsn: string;
  lag_bytes: number;
  lag_formatted: string;
  tables_total: number;
  tables_copied: number;
  tables: TableProgress[];
  rows_per_sec: number;
  bytes_per_sec: number;
  total_rows: number;
  total_bytes: number;
  error_count: number;
  last_error?: string;
}

interface TableProgress {
  schema: string;
  name: string;
  status: "pending" | "copying" | "copied" | "streaming";
  rows_total: number;
  rows_copied: number;
  size_bytes: number;
  bytes_copied: number;
  percent: number;
  elapsed_sec: number;
}

interface LogEntry {
  time: string;
  level: string;
  message: string;
  fields?: Record<string, string>;
}
```

## API Layer

### REST Client (`api/client.ts`)

Three async functions for polling:

- **`fetchStatus()`** — `GET /api/v1/status` → `Snapshot`
- **`fetchTables()`** — `GET /api/v1/tables` → `TableProgress[]`
- **`fetchLogs()`** — `GET /api/v1/logs` → `LogEntry[]`

All use relative URLs (empty base), so they work both with the Vite proxy and when served from the Go binary.

### WebSocket Connection (`api/websocket.ts`)

The `WSConnection` class manages a persistent WebSocket connection with automatic reconnection:

```typescript
class WSConnection {
  constructor(onSnapshot: OnSnapshot, onStatus: OnStatus)
  close(): void
}
```

**Connection Logic:**
1. Constructs WebSocket URL from `window.location` (auto-detects `ws:` or `wss:`)
2. Connects to `/api/v1/ws`
3. On `onopen`: reports connected status, resets reconnect delay
4. On `onmessage`: parses JSON snapshot, calls `onSnapshot` callback
5. On `onclose`: reports disconnected status, schedules reconnect
6. On `onerror`: closes the socket (triggers `onclose` → reconnect)

**Exponential Backoff:**
- Initial delay: 1 second
- Doubles on each failure
- Maximum delay: 30 seconds
- Resets to 1 second on successful connection

**Cleanup:**
- `close()` sets a `closed` flag that prevents reconnection attempts
- Called automatically by the React hook's cleanup function

## React Hook (`hooks/useMetrics.ts`)

```typescript
function useMetrics(): {
  snapshot: Snapshot | null;
  connected: boolean;
  history: Snapshot[];
}
```

The `useMetrics` hook manages the entire real-time data lifecycle:

1. Creates a `WSConnection` on mount
2. On each incoming snapshot: updates `snapshot` state and appends to `history`
3. History is capped at 300 entries (sliding window for chart data)
4. Tracks connection status (connected/disconnected)
5. Cleans up WebSocket on unmount

All callbacks are wrapped in `useCallback` for referential stability.

## Components

### `App.tsx`

Root component. Shows a loading spinner with "Connecting to migration server..." until the first snapshot arrives, then renders the `Dashboard`.

### `Dashboard.tsx`

Main layout using the dark theme (`bg-gray-950`):

```
Header (fixed)
  └── pgmigrator title + connection indicator + PhaseIndicator
Main content (max-w-7xl centered)
  ├── ThroughputCard (4-column grid)
  ├── ProgressBar
  ├── LagChart + LogViewer (2-column grid on lg:)
  └── TableProgress
Footer
  └── "pgmigrator — PostgreSQL online migration tool"
```

### `PhaseIndicator.tsx`

Displays the current migration phase as a colored badge:

| Phase | Color | Style |
|-------|-------|-------|
| idle | `bg-gray-600` | Gray pill |
| connecting | `bg-yellow-500` | Yellow pill |
| schema | `bg-blue-500` | Blue pill |
| copy | `bg-purple-500` | Purple pill |
| streaming | `bg-green-500` | Green pill |
| switchover | `bg-orange-500` | Orange pill |
| switchover-complete | `bg-green-600` | Dark green pill |
| done | `bg-green-700` | Darker green pill |

Also shows elapsed time and error count (if any).

### `ProgressBar.tsx`

A full-width progress bar showing overall table copy completion:
- Background: `bg-gray-700` rounded container
- Fill: `bg-purple-500` with CSS transition animation
- Label: "X/Y tables (Z.Z%)"
- Width controlled by CSS `style={{ width: '${pct}%' }}`

### `ThroughputCard.tsx`

A 2x2 (or 4x1 on wide screens) grid of metric cards:

| Card | Color | Value |
|------|-------|-------|
| Rows/sec | `text-blue-400` | Formatted number |
| Bytes/sec | `text-green-400` | KB/MB/GB format |
| Total Rows | `text-purple-400` | K/M/B format |
| Total Data | `text-yellow-400` | KB/MB/GB format |

Each card has a small uppercase label and a large bold value.

### `TableProgress.tsx`

A sortable HTML table with per-row progress bars:

| Column | Content |
|--------|---------|
| Table | `schema.name` in monospace |
| Status | Color-coded: pending (gray), copying (yellow), copied (green), streaming (blue "⟳ live") |
| Rows | `copied/total` with smart formatting, or "—" for streaming |
| Size | Human-readable bytes |
| Progress | Mini progress bar (gray background, color-coded fill) + percentage |

Rows have hover highlight (`hover:bg-gray-700/30`).

### `LagChart.tsx`

A Recharts `LineChart` showing replication lag over time:

- Shows the last 60 data points from the snapshot history
- X-axis: formatted time (`toLocaleTimeString`)
- Y-axis: bytes with smart formatting
- Line: purple (`#8B5CF6`), 2px stroke, no dots
- Tooltip: dark background matching the theme
- Responsive: fills container width, 200px height
- Shows "Waiting for data..." when no history is available

### `LogViewer.tsx`

An auto-scrolling log viewer that polls `GET /api/v1/logs` every 2 seconds:

- Maximum height: 192px (12rem) with overflow scroll
- Monospace font, 12px size
- Auto-scrolls to bottom when new entries arrive (via `useRef` + `scrollTop`)
- Color-coded levels: debug (gray), info (blue), warn (yellow), error (red)
- Timestamp in `toLocaleTimeString` format

## Tailwind Configuration

```javascript
// tailwind.config.js
content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"]
```

Uses Tailwind v3 with default theme. The dark color scheme uses Tailwind's gray scale (gray-950 through gray-100).

## Vite Configuration

```typescript
// vite.config.ts
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { '/api': 'http://localhost:7654' }
  },
  build: { outDir: 'dist', emptyOutDir: true }
})
```

- **Dev proxy**: All `/api/*` requests are forwarded to the Go server during development
- **Build output**: `dist/` directory, which is then copied to `internal/server/dist/` for embedding

## Dependencies

### Runtime
| Package | Version | Purpose |
|---------|---------|---------|
| `react` | ^18.3.1 | UI framework |
| `react-dom` | ^18.3.1 | DOM rendering |
| `recharts` | ^2.12.7 | Charting library (LineChart for lag) |

### Development
| Package | Version | Purpose |
|---------|---------|---------|
| `typescript` | ^5.6.3 | Type checking |
| `vite` | ^6.0.3 | Build tool and dev server |
| `@vitejs/plugin-react` | ^4.3.4 | React Fast Refresh for Vite |
| `tailwindcss` | ^3.4.16 | Utility-first CSS |
| `postcss` | ^8.4.49 | CSS processing |
| `autoprefixer` | ^10.4.20 | CSS vendor prefixes |
| `@types/react` | ^18.3.12 | React type definitions |
| `@types/react-dom` | ^18.3.1 | ReactDOM type definitions |
