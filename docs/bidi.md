# Bidirectional Replication (Loop Detection)

**Package:** `internal/migration/bidi`
**File:** `bidi.go`

## Overview

The bidi package provides loop detection for bidirectional replication scenarios. When pgmanager is used to replicate in both directions (A → B and B → A), changes applied to database B by the A→B pipeline would be picked up by B's WAL stream and sent back to A, creating an infinite loop. The `Filter` breaks this loop by dropping messages that originated from a known replication origin.

## How Loop Detection Works

PostgreSQL's replication origin feature (`pg_replication_origin`) allows tagging writes with an origin name. When pgmanager applies changes to the destination, it tags those writes with an origin ID. The filter then drops any WAL messages that carry that same origin ID, because those messages are "echoes" of changes that pgmanager itself applied.

```
Database A ──(WAL)──► Decoder A ──► Filter A (drops origin "pgmanager-b") ──► Applier A ──► Database B
                                                                                    │
                                                                              tags writes with
                                                                              origin "pgmanager-a"

Database B ──(WAL)──► Decoder B ──► Filter B (drops origin "pgmanager-a") ──► Applier B ──► Database A
                                                                                    │
                                                                              tags writes with
                                                                              origin "pgmanager-b"
```

## Types

### `Filter`

```go
type Filter struct {
    originID string          // Origin ID to filter out
    logger   zerolog.Logger  // Component-tagged logger
}
```

### `Manager`

A placeholder for full bidirectional replication setup:

```go
type Manager struct {
    OriginA string          // Origin ID for A→B direction
    OriginB string          // Origin ID for B→A direction
    logger  zerolog.Logger
}
```

## Filter

### Construction

```go
filter := bidi.NewFilter("pgmanager-origin", logger)
```

Creates a filter that will drop any message whose `OriginID()` matches the provided string.

### Running

```go
func (f *Filter) Run(ctx context.Context, in <-chan stream.Message) <-chan stream.Message
```

`Run` is a streaming filter that:

1. Creates an output channel with the same buffer capacity as the input channel
2. Launches a goroutine that reads from `in` and writes to `out`
3. For each message:
   - If `msg.OriginID() == f.originID` and `f.originID != ""` → **drop** (log at DEBUG level)
   - Otherwise → **forward** to output channel
4. Stops when either:
   - The input channel is closed
   - The context is cancelled

```go
go func() {
    defer close(out)
    for {
        select {
        case <-ctx.Done():
            return
        case msg, ok := <-in:
            if !ok {
                return
            }
            if msg.OriginID() == f.originID && f.originID != "" {
                // Dropped: this message originated from our own writes
                continue
            }
            out <- msg
        }
    }
}()
```

### Safety

- The empty-string check (`f.originID != ""`) prevents accidentally filtering all messages when origin tracking isn't configured
- The filter is a pure passthrough when `originID` is empty — no messages are dropped
- Debug logging on dropped messages helps with troubleshooting replication loops

## Manager

### Construction

```go
manager := bidi.NewManager("origin-a", "origin-b", logger)
```

### Start

```go
func (m *Manager) Start(ctx context.Context) error
```

Currently a placeholder that logs the configuration and blocks until the context is cancelled. In a full implementation, this would:

1. Create two decoder→filter→applier chains
2. Configure replication origins on both databases
3. Start both directions concurrently
4. Monitor both directions and report errors

## Pipeline Integration

The filter is optionally inserted between the decoder output and the applier input:

```go
// In Pipeline.initComponents():
if p.cfg.Replication.OriginID != "" {
    p.bidiFilter = bidi.NewFilter(p.cfg.Replication.OriginID, p.logger)
}

// In Pipeline.RunCloneAndFollow() and RunFollow():
var applierCh <-chan stream.Message = buffered
if p.bidiFilter != nil {
    applierCh = p.bidiFilter.Run(ctx, buffered)
}
return p.applier.Start(ctx, applierCh, onApplied)
```

When no `--origin-id` flag is provided, the filter is not created and the decoder output goes directly to the applier — zero overhead for unidirectional migrations.

## CLI Usage

```bash
# A→B direction: tag writes with "pgmanager-a", filter out "pgmanager-b"
pgmanager follow --origin-id=migrator-b \
    --source-host=db-a --dest-host=db-b

# B→A direction: tag writes with "pgmanager-b", filter out "pgmanager-a"
pgmanager follow --origin-id=migrator-a \
    --source-host=db-b --dest-host=db-a
```

The origin tagging on the destination side is handled by the `pgwire` package's `SetReplicationOrigin()` method.

## Origin Tracking in the WAL Stream

When PostgreSQL produces WAL records for writes made within a replication origin session, it includes an `OriginMessage` in the logical replication stream. The decoder captures this:

```go
case *pglogrepl.OriginMessage:
    d.origin = msg.Name
```

Subsequent `ChangeMessage` objects carry this origin:

```go
d.emit(ctx, ch, &ChangeMessage{
    ...
    Origin: d.origin,
})
```

The filter then checks `msg.OriginID()` which returns this `Origin` field.
