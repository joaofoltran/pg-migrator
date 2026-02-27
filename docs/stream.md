# Stream (Message & Decoder)

**Package:** `internal/stream`
**Files:** `message.go`, `decoder.go`

## Overview

The stream package is the architectural spine of pgmigrator. It defines the unified `Message` interface through which all data flows — both real WAL changes and synthetic sentinels — and the `Decoder` that consumes the PostgreSQL logical replication stream and converts it into these messages.

This package implements the fundamental design decision: a single `chan Message` carries everything through the pipeline. There are no separate control channels, side bands, or out-of-band communication. This simplifies the architecture and makes the pipeline easy to reason about.

## Message Interface

```go
type Message interface {
    Kind() MessageKind
    LSN() pglogrepl.LSN
    OriginID() string
    Timestamp() time.Time
}
```

Every message flowing through the pipeline implements this interface. The four methods provide:

- **`Kind()`** — Discriminator for type-switching in the applier
- **`LSN()`** — Log Sequence Number for ordering and confirmation
- **`OriginID()`** — Replication origin for bidi loop detection (empty for most messages)
- **`Timestamp()`** — When the event occurred (source commit time or injection time)

## Message Kinds

```go
const (
    KindBegin    MessageKind = iota  // 0 — Transaction start
    KindCommit                        // 1 — Transaction end
    KindChange                        // 2 — INSERT, UPDATE, or DELETE
    KindRelation                      // 3 — Schema metadata for a table
    KindSentinel                      // 4 — Synthetic switchover marker
)
```

Each kind has a `String()` method returning the human-readable name.

## Message Types

### `BeginMessage`

Marks the start of a transaction from the source WAL.

| Field    | Type            | Description                                 |
|----------|-----------------|---------------------------------------------|
| `TxnLSN` | `pglogrepl.LSN` | Final LSN of this transaction               |
| `TxnTime`| `time.Time`     | Commit timestamp from the source             |
| `XID`    | `uint32`        | Transaction ID                               |

- `Kind()` → `KindBegin`
- `LSN()` → `TxnLSN`
- `OriginID()` → `""` (always empty)
- `Timestamp()` → `TxnTime`

### `CommitMessage`

Marks the end of a transaction.

| Field      | Type            | Description                                |
|------------|-----------------|-------------------------------------------|
| `CommitLSN`| `pglogrepl.LSN` | LSN of the commit record                   |
| `TxnTime`  | `time.Time`     | Commit timestamp                            |

- `Kind()` → `KindCommit`
- `LSN()` → `CommitLSN`
- `OriginID()` → `""` (always empty)
- `Timestamp()` → `TxnTime`

### `RelationMessage`

Carries schema metadata for a table. Sent by PostgreSQL before the first change to a relation in a given session, and again whenever the schema changes.

| Field        | Type            | Description                               |
|--------------|-----------------|------------------------------------------|
| `RelationID` | `uint32`        | PostgreSQL internal relation OID           |
| `Namespace`  | `string`        | Schema name (e.g., `public`)              |
| `Name`       | `string`        | Table name                                 |
| `Columns`    | `[]Column`      | Column definitions (name + data type OID) |
| `MsgLSN`    | `pglogrepl.LSN` | WAL position of this message               |
| `MsgTime`   | `time.Time`     | When this message was received             |

The decoder caches `RelationMessage` by `RelationID` so that subsequent `ChangeMessage` entries can reference column metadata.

### `ChangeMessage`

Represents an INSERT, UPDATE, or DELETE operation.

| Field        | Type            | Description                                |
|--------------|-----------------|-------------------------------------------|
| `Op`         | `ChangeOp`      | Operation type: `OpInsert`, `OpUpdate`, `OpDelete` |
| `RelationID` | `uint32`        | References the relation (for schema lookup) |
| `Namespace`  | `string`        | Schema name                                |
| `Table`      | `string`        | Table name                                 |
| `OldTuple`   | `*TupleData`    | Old row values (for UPDATE/DELETE with replica identity) |
| `NewTuple`   | `*TupleData`    | New row values (for INSERT/UPDATE)          |
| `MsgLSN`    | `pglogrepl.LSN` | WAL position                                |
| `MsgTime`   | `time.Time`     | Reception timestamp                         |
| `Origin`    | `string`        | Replication origin name (for bidi)          |

- `OriginID()` → `Origin` (non-empty when origin tracking is active)

### `ChangeOp`

```go
const (
    OpInsert ChangeOp = iota  // 0
    OpUpdate                   // 1
    OpDelete                   // 2
)
```

Each has a `String()` method: `"INSERT"`, `"UPDATE"`, `"DELETE"`.

### Tuple Data

```go
type TupleData struct {
    Columns []Column
}

type Column struct {
    Name     string
    DataType uint32
    Value    []byte
}
```

Column values are raw bytes as received from the WAL stream. The applier passes them as string values to parameterized queries, letting pgx handle type conversion.

## Decoder

The `Decoder` connects to the source PostgreSQL via the replication protocol and converts raw WAL data into `Message` objects on a channel.

### Construction

```go
decoder := stream.NewDecoder(replConn, slotName, publication, logger)
```

| Parameter    | Description                                         |
|-------------|-----------------------------------------------------|
| `replConn`  | Raw `pgconn.PgConn` with `replication=database`    |
| `slotName`  | Replication slot name (e.g., `pgmigrator`)          |
| `publication`| Publication name (e.g., `pgmigrator_pub`)          |
| `logger`    | zerolog logger, tagged with component `decoder`      |

### Starting

```go
msgCh, snapshotName, err := decoder.Start(ctx, startLSN)
```

**When `startLSN == 0` (new migration):**
1. Creates a new replication slot via `pglogrepl.CreateReplicationSlot`
2. Captures the `SnapshotName` for consistent COPY
3. Parses the `ConsistentPoint` LSN string
4. Starts replication from the consistent point

**When `startLSN > 0` (resuming):**
1. Starts replication from the given LSN (slot must already exist)
2. `snapshotName` is empty

**In both cases:**
1. Calls `pglogrepl.StartReplication` with pgoutput plugin args: `proto_version '1'` and `publication_names '<name>'`
2. Starts the `receiveLoop` goroutine
3. Returns a buffered channel (256 capacity) for messages

### Receive Loop

The core goroutine (`receiveLoop`) runs continuously:

```
while not cancelled:
    1. Send standby status if 10s since last send
    2. ReceiveMessage with 10s deadline
    3. Handle:
       - PrimaryKeepaliveMessage → reply if requested
       - XLogData → decode WAL data → emit messages
       - Timeout → loop again
       - Error → log and exit
```

**Standby Status Updates:**
- Sent every 10 seconds with the current confirmed LSN
- Also sent in response to `ReplyRequested` keepalive messages
- This prevents the replication slot from being dropped due to inactivity

**WAL Data Decoding:**

The `decodeWALData` method parses `pglogrepl` logical messages:

| pglogrepl Type | → Message Type | Notes |
|---------------|----------------|-------|
| `BeginMessage` | `BeginMessage` | Extracts FinalLSN, CommitTime, Xid |
| `CommitMessage` | `CommitMessage` | Extracts CommitLSN, CommitTime |
| `RelationMessage` | `RelationMessage` | Caches in `d.relations` map, extracts columns |
| `InsertMessage` | `ChangeMessage` (OpInsert) | Looks up relation, decodes new tuple |
| `UpdateMessage` | `ChangeMessage` (OpUpdate) | Looks up relation, decodes old+new tuples |
| `DeleteMessage` | `ChangeMessage` (OpDelete) | Looks up relation, decodes old tuple |
| `OriginMessage` | (internal) | Sets `d.origin` for subsequent messages |

**Tuple Decoding (`decodeTuple`):**
- Maps `pglogrepl.TupleData.Columns` to `stream.Column` structs
- Copies column names and data types from the cached `RelationMessage`
- Handles the case where tuple has more columns than the cached relation

### LSN Confirmation

```go
decoder.ConfirmLSN(lsn)
```

Atomically advances the confirmed flush position. The next standby status update will report this LSN to PostgreSQL, allowing the server to reclaim WAL storage.

Thread-safe: uses a mutex and only advances (never decrements).

### Shutdown

```go
decoder.Close()
```

1. Cancels the receive loop's context
2. Blocks on `<-d.done` until the receive loop exits
3. The receive loop closes the message channel on exit, signaling downstream consumers

### Emit Helper

```go
func (d *Decoder) emit(ctx context.Context, ch chan<- Message, msg Message) {
    select {
    case ch <- msg:
    case <-ctx.Done():
    }
}
```

Non-blocking send: if the channel is full and the context is cancelled, the message is dropped rather than blocking forever.

## Data Flow Through the Pipeline

```
PostgreSQL WAL
     │
     ▼
pglogrepl.ReceiveMessage
     │
     ▼
decodeWALData() ──► emit() ──► chan Message (256 buffer)
     │                              │
     │                              ▼
     │                         bidi.Filter (optional)
     │                              │
     │                              ▼
     │                         replay.Applier
     │                              │
     │                              ▼
     │                         onApplied callback
     │                              │
     └──────── ConfirmLSN() ◄───────┘
                    │
                    ▼
            StandbyStatusUpdate ──► PostgreSQL
```
