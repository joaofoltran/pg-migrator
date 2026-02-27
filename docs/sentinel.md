# Sentinel (Switchover Coordinator)

**Package:** `internal/sentinel`
**File:** `sentinel.go`

## Overview

The sentinel package implements pgmigrator's zero-downtime switchover mechanism. It works by injecting a synthetic marker message into the pipeline and waiting for it to be confirmed by the applier. When the sentinel is confirmed, it proves that the destination database has applied all WAL changes up to the point of injection — the destination is fully caught up and ready to serve traffic.

This is fundamentally different from lag-based switchover (checking if lag is below a threshold), which is inherently racy. The sentinel approach provides a cryptographic proof of consistency: the destination has seen everything the source produced up to that exact LSN.

## Architecture

```
                    Coordinator.Initiate()
                          │
                          ▼
              ┌──── chan Message ────┐
              │   (SentinelMessage)  │
              │                      │
              ▼                      │
         Applier sees               │
         SentinelMessage             │
              │                      │
              ▼                      │
     Coordinator.Confirm()           │
              │                      │
              ▼                      │
     WaitForConfirmation()           │
         returns nil                 │
              │                      │
              ▼                      │
     "Destination is caught up"      │
```

## Types

### `SentinelMessage`

A synthetic message that implements the `stream.Message` interface:

```go
type SentinelMessage struct {
    ID      string            // Unique identifier (e.g., "sentinel-1")
    SentLSN pglogrepl.LSN     // LSN at time of injection
    SentAt  time.Time         // Timestamp of injection
}
```

| Method | Returns | Description |
|--------|---------|-------------|
| `Kind()` | `stream.KindSentinel` | Identifies as sentinel type |
| `LSN()` | `SentLSN` | The LSN position when injected |
| `OriginID()` | `""` | Always empty (sentinels are internal) |
| `Timestamp()` | `SentAt` | Injection timestamp |

The sentinel flows through the same `chan Message` as regular WAL changes. Because channels are FIFO, the sentinel is guaranteed to be processed after all previously queued changes.

### `Coordinator`

Manages sentinel injection and confirmation:

```go
type Coordinator struct {
    logger  zerolog.Logger
    out     chan<- stream.Message          // Pipeline message channel
    mu      sync.Mutex
    pending map[string]chan confirmation   // Waiting sentinels
    nextID  int                            // Auto-incrementing ID counter
}
```

### `confirmation`

Internal type holding the confirmation timestamp:

```go
type confirmation struct {
    confirmedAt time.Time
}
```

## Lifecycle

### Construction

```go
coordinator := sentinel.NewCoordinator(messagesCh, logger)
```

Creates a coordinator that writes sentinel messages to the provided channel. The channel must be the same one consumed by the applier.

### Injection (`Initiate`)

```go
func (c *Coordinator) Initiate(ctx context.Context, lsn pglogrepl.LSN) (string, error)
```

1. Generates a unique ID: `sentinel-1`, `sentinel-2`, etc. (auto-incrementing, mutex-protected)
2. Creates a buffered confirmation channel (capacity 1) and stores it in the `pending` map
3. Constructs a `SentinelMessage` with the current LSN and timestamp
4. Sends the message to the pipeline channel:
   - If the channel accepts, returns the sentinel ID
   - If the context is cancelled (pipeline shutting down), cleans up and returns the error

### Waiting (`WaitForConfirmation`)

```go
func (c *Coordinator) WaitForConfirmation(id string, timeout time.Duration) error
```

Blocks until one of:
- The confirmation channel receives a value → returns `nil` (success)
- The timeout elapses → cleans up the pending entry, returns timeout error

The caller (pipeline's `RunSwitchover`) uses this to block until the destination is caught up.

### Confirmation (`Confirm`)

```go
func (c *Coordinator) Confirm(id string)
```

Called by the applier when it encounters a `SentinelMessage` in the message stream. Sends a `confirmation` struct to the pending channel and removes the entry from the map.

If the ID is not found in the pending map (e.g., already timed out), the confirmation is silently ignored.

## Switchover Flow

Here's the complete switchover sequence:

```
1. User runs: pgmigrator switchover --timeout 30s

2. Pipeline.RunSwitchover():
   a. Gets current applied LSN from applier
   b. Calls coordinator.Initiate(ctx, currentLSN)
      → Sentinel injected into chan Message
   c. Calls coordinator.WaitForConfirmation(id, 30s)
      → Blocks...

3. Meanwhile, the applier processes messages in order:
   - BeginMessage → BEGIN
   - ChangeMessage → INSERT/UPDATE/DELETE
   - CommitMessage → COMMIT
   - ... more transactions ...
   - SentinelMessage → coordinator.Confirm(id)

4. WaitForConfirmation unblocks → returns nil

5. Pipeline transitions to "switchover-complete"
   → "switchover confirmed — destination is caught up"
```

## Why This Works

The sentinel mechanism provides a strong consistency guarantee because of channel FIFO ordering:

1. The sentinel is injected **after** all currently queued messages
2. The pipeline channel is a Go channel, which is strictly ordered
3. The applier processes messages sequentially
4. Therefore, when the applier sees the sentinel, it has already processed all messages that were in the channel before the sentinel
5. Combined with WAL ordering guarantees, this means the destination has applied all changes up to the sentinel's LSN

## Thread Safety

- `nextID` is protected by `mu` for concurrent injection (though typically only one switchover runs at a time)
- The `pending` map is protected by `mu` for concurrent Initiate/Confirm/WaitForConfirmation calls
- The confirmation channel is buffered (capacity 1) so `Confirm` never blocks

## Error Scenarios

| Scenario | Behavior |
|----------|----------|
| Pipeline channel full | `Initiate` blocks until space is available or context is cancelled |
| Pipeline shut down during wait | Context cancellation causes `Initiate` to clean up and return error |
| Timeout elapsed | `WaitForConfirmation` returns error, removes pending entry |
| Double confirmation | Second `Confirm` call finds no pending entry, silently no-ops |
| Unknown sentinel ID | `WaitForConfirmation` returns "unknown sentinel" error |

## Logging

All sentinel events are logged at INFO level:
- `"sentinel injected"` — with ID and LSN
- `"sentinel confirmed"` — with ID and round-trip duration
