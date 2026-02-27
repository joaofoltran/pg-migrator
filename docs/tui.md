# Terminal UI (TUI)

**Package:** `internal/tui`
**Files:** `app.go`, `styles.go`, `components/header.go`, `components/progress.go`, `components/tables.go`, `components/lag.go`, `components/throughput.go`, `components/logs.go`

## Overview

The TUI provides a real-time terminal dashboard for monitoring pgmigrator migrations. Built on [Bubble Tea](https://github.com/charmbracelet/bubbletea) (the Elm Architecture for Go terminals) with [Lipgloss](https://github.com/charmbracelet/lipgloss) styling, it renders a full-screen dashboard that updates every 500ms with live metrics from the pipeline.

The TUI can operate in two modes:
1. **In-process** — Reads directly from the `metrics.Collector` via Go channels (zero network overhead)
2. **Remote** — Polls a running pipeline's HTTP API (`pgmigrator tui --api-addr=http://host:port`)

## Dashboard Layout

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
│  public.products        0/450K     89 MB    ░░░░   0%│
│  public.events          STREAMING  —        ⟳ live   │
│  ...                                                   │
├───────────────────────────────────────────────────────┤
│  Lag: 1.2 MB  ▁▂▃▄▅▆▇█▇▆▅▄▃▂▁▁▂▃▄▅▆▇▆▅▄▃▂▁        │
├───────────────────────────────────────────────────────┤
│  4,521 rows/s  |  1.0 MB/s  |  Total: 22.7M rows     │
├───────────────────────────────────────────────────────┤
│  14:23:01 INF table copy complete table=public.users   │
│  14:23:02 INF starting copy table=public.orders        │
│  14:23:05 INF applied transaction lsn=0/1A3B4C5        │
└───────────────────────────────────────────────────────┘
  q: quit
```

The dashboard consists of seven vertically stacked sections, each rendered by a dedicated component function. All sections are wrapped in rounded-border boxes using Lipgloss.

## Main Model (`app.go`)

### Bubble Tea Architecture

The TUI follows the Elm Architecture pattern:

1. **Model** — Holds all state: the collector reference, current snapshot, lag history, terminal dimensions
2. **Init** — Subscribes to the collector and starts waiting for the first snapshot
3. **Update** — Handles three message types: key presses, window resize, snapshot updates
4. **View** — Renders the complete dashboard by composing all component outputs

### Model Struct

```go
type Model struct {
    collector  *metrics.Collector   // Data source
    sub        chan metrics.Snapshot // Subscription channel
    snapshot   metrics.Snapshot     // Latest snapshot
    lagHistory *components.LagHistory // Rolling lag values for sparkline
    width      int                  // Terminal width
    height     int                  // Terminal height
    ready      bool                 // True after first WindowSizeMsg
}
```

### Message Flow

```
Collector.broadcastLoop() ──500ms──► chan Snapshot ──► waitForSnapshot() ──► snapshotMsg
                                                                                │
                                                                         Update() processes
                                                                                │
                                                                         View() re-renders
```

The `waitForSnapshot` function returns a `tea.Cmd` that blocks on the subscription channel. When a snapshot arrives, it's converted to a `snapshotMsg` and processed by `Update()`, which stores the new snapshot and immediately issues another `waitForSnapshot` command — creating a continuous update loop.

### Key Bindings

| Key       | Action |
|-----------|--------|
| `q`       | Quit — unsubscribes from collector, exits TUI |
| `Ctrl+C`  | Quit — same as `q` |

### Rendering

The `View()` method composes all sections vertically:

1. **Title bar** — Full-width purple background with "pgmigrator" text
2. **Header** — Phase, elapsed time, lag, and throughput summary
3. **Progress** — Overall completion bar
4. **Table list** — Per-table progress (height adapts to terminal size)
5. **Lag sparkline** — Rolling lag visualization
6. **Throughput** — Rows/sec, bytes/sec, totals, errors
7. **Logs** — Last 5 log entries
8. **Help** — Key binding hints

Each section is wrapped in `boxStyle` (rounded border, gray border color, horizontal padding).

### Running the TUI

```go
// In-process (from clone/follow --tui)
err := tui.Run(pipeline.Metrics)

// This starts fullscreen alt-screen mode and blocks until quit
```

The `Run()` function creates a `NewModel`, wraps it in a `tea.NewProgram` with `tea.WithAltScreen()`, and calls `p.Run()`.

## Color Theme (`styles.go`)

The TUI uses a consistent dark theme designed for modern terminal emulators with true color support:

### Color Palette

| Name          | Hex       | Usage                                      |
|---------------|-----------|-------------------------------------------|
| `colorPrimary`| `#7C3AED` | Purple — title bar, progress, branding     |
| `colorSuccess`| `#10B981` | Green — completed tables, low lag          |
| `colorWarning`| `#F59E0B` | Amber — copying tables, medium lag         |
| `colorDanger` | `#EF4444` | Red — errors, high lag                     |
| `colorInfo`   | `#3B82F6` | Blue — streaming, table headers, info logs |
| `colorMuted`  | `#6B7280` | Gray — labels, help text, pending items    |
| `colorBg`     | `#1F2937` | Dark gray — backgrounds                    |
| `colorBorder` | `#374151` | Border gray — box borders                  |
| `colorHighlight`| `#A78BFA`| Light purple — phase indicator             |

### Style Definitions

All styles are defined as package-level `lipgloss.Style` variables:

- `titleStyle` — Bold purple text with bottom margin
- `headerStyle` — White on purple for the title bar
- `phaseStyle` — Bold light purple for phase names
- `valueStyle` — White for metric values
- `labelStyle` — Muted gray for labels
- `boxStyle` — Rounded border with gray border color and horizontal padding
- `progressFullStyle` / `progressEmptyStyle` — Green/gray for progress bars
- `tableHeaderStyle` — Bold blue with bottom border
- `statusCopyingStyle` / `statusCopiedStyle` / `statusStreamingStyle` / `statusPendingStyle` — Per-status colors
- `logINFStyle` / `logWRNStyle` / `logERRStyle` — Per-level log colors
- `helpStyle` — Muted gray for help text

## Components

### Header (`components/header.go`)

**`RenderHeader(snap metrics.Snapshot, width int) string`**

Renders a single-line status bar with left-aligned and right-aligned sections:

```
  Phase: STREAMING    Elapsed: 1h 23m 45s              Lag: 1.2 MB    Throughput: 4,521 rows/s
```

- Phase is rendered in bold light purple, uppercased
- Elapsed time is formatted as `Xh Xm Xs`, `Xm Xs`, or `Xs` depending on duration
- The gap between left and right sections is dynamically calculated to fill the terminal width
- Uses `lipgloss.Width()` to correctly measure ANSI-escaped string widths

**`formatDuration(seconds float64) string`** — Helper that converts seconds to human-readable duration.

### Progress Bar (`components/progress.go`)

**`RenderProgress(snap metrics.Snapshot, width int) string`**

Renders an overall progress bar showing tables copied vs. total:

```
  Overall: ████████████████████░░░░  78.0% (42/54 tables)
```

- Bar width adapts to terminal width (minimum 10 characters)
- Filled portion is green (`#10B981`), empty portion is dark gray (`#374151`)
- Shows percentage and fraction
- Returns "No tables to copy" when `TablesTotal` is 0

### Table Progress (`components/tables.go`)

**`RenderTables(snap metrics.Snapshot, width int, maxRows int) string`**

Renders a columnar table showing per-table migration progress:

```
  Table                             Rows              Size       Progress
  public.users                      1.2M/1.2M         256.0 MB   ████████████  100%
  public.orders                     890.0K/2.1M       1.1 GB     ██████░░░░░░  42.3%
  public.products                   0/450.0K          89.0 MB    ░░░░░░░░░░░░    0%
  public.events                     STREAMING          —          ⟳ live
```

- Table names are truncated to 33 characters with `...` suffix
- Row counts use smart formatting: 1.2M, 450.0K, or raw number
- Size uses KB/MB/GB formatting
- Mini progress bars are 12 characters wide using `█` and `░`
- Color-coded by status: amber for copying, green for copied, blue for streaming, gray for pending
- Supports `maxRows` parameter — shows "... and N more tables" when truncated
- `maxRows` is dynamically calculated from terminal height (reserves 18 lines for other sections)

**`miniBar(pct float64, width int) string`** — Generates a text progress bar of the specified width.

**`formatCount(n int64) string`** — Smart number formatting: `1.2B`, `4.5M`, `890.0K`, or raw.

**`formatBytes(b int64) string`** — Size formatting: `1.1 GB`, `256.0 MB`, `89.0 KB`, `512 B`.

### Lag Sparkline (`components/lag.go`)

**`RenderLag(snap metrics.Snapshot, history *LagHistory, width int) string`**

Renders the replication lag with a color-coded value and sparkline graph:

```
  Lag: 1.20 MB (latency: 0s)  ▁▂▃▄▅▆▇█▇▆▅▄▃▂▁▁▂▃▄▅▆▇▆▅▄▃▂▁
```

- Lag value color changes based on severity:
  - Green (`#10B981`) — under 1 MB
  - Amber (`#F59E0B`) — 1 MB to 10 MB
  - Red (`#EF4444`) — over 10 MB
- Sparkline uses 8 Unicode block characters: `▁▂▃▄▅▆▇█`
- Width adapts to terminal (minimum 10 characters for sparkline)

**`LagHistory`** — A fixed-capacity ring buffer for lag values:

```go
type LagHistory struct {
    values []uint64
    cap    int
}
```

- `NewLagHistory(cap)` — Creates a buffer (default: 60 entries = 30 seconds of data)
- `Push(lag)` — Adds a value, shifting the buffer when full
- `Sparkline(width)` — Generates a sparkline string:
  1. Takes the last `width` values
  2. Finds the maximum value for normalization
  3. Maps each value to one of 8 sparkline characters
  4. Pads with `▁` if fewer values than width

### Throughput (`components/throughput.go`)

**`RenderThroughput(snap metrics.Snapshot, width int) string`**

Renders throughput counters in a single line:

```
  4,521 rows/s  |  1.0 MB/s  |  Total: 22.7M rows, 5.0 GB
```

- Rows/sec and bytes/sec are bold white
- Shows error count in red if any errors have occurred
- Reuses `formatCount` and `formatBytes` helpers from the tables component

### Log Viewer (`components/logs.go`)

**`RenderLogs(entries []metrics.LogEntry, maxLines int) string`**

Renders the most recent log entries:

```
  14:23:01 INF table copy complete table=public.users
  14:23:02 INF starting copy table=public.orders
  14:23:05 INF applied transaction lsn=0/1A3B4C5
```

- Shows the last `maxLines` entries (default: 5 in the main view)
- Timestamps in `HH:MM:SS` format, gray
- Log levels are color-coded:
  - `INF` — blue (`#3B82F6`)
  - `WRN` — amber (`#F59E0B`)
  - `ERR` — red (`#EF4444`)
  - `DBG` — gray (`#6B7280`)
- Returns "No log entries yet" when the buffer is empty

## CLI Integration

### In-Process Mode (`--tui` flag)

```bash
pgmigrator clone --follow --tui
pgmigrator follow --tui
```

When `--tui` is passed:
1. The pipeline is started in a background goroutine
2. `tui.Run(pipeline.Metrics)` is called on the main goroutine (it must run on the main thread for terminal I/O)
3. The TUI reads directly from the collector's subscription channel — no HTTP overhead
4. When the user quits the TUI, the pipeline error (if any) is returned

### Remote Mode (`tui` command)

```bash
pgmigrator tui --api-addr=http://production-host:7654
```

1. Creates a local `metrics.Collector` (not connected to any pipeline)
2. Starts a polling goroutine that fetches `GET /api/v1/status` every 500ms
3. Each fetched `Snapshot` is used to update the local collector's phase and tables
4. The TUI renders from this local collector, identical to in-process mode
5. API errors are recorded via `collector.RecordError()` and shown in the TUI

## Performance

- Rendering is purely CPU-bound string construction — no I/O during `View()`
- Snapshot reads use `RLock` (multiple concurrent readers)
- The 500ms update interval keeps CPU usage negligible even on large table lists
- Terminal output is batched by Bubble Tea (double-buffered alt screen)
