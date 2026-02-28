# Configuration

**Package:** `internal/config`
**File:** `config.go`

## Overview

The config package defines typed configuration structs for all migrator settings. It provides DSN builders that construct PostgreSQL connection strings from individual fields, and a validation method that checks for required fields and applies sensible defaults. Configuration flows from CLI flags into these structs via cobra's flag binding.

## Architecture

```
CLI Flags (cobra)
      │
      ▼
 config.Config
      │
      ├── Source: DatabaseConfig ──► DSN() / ReplicationDSN()
      ├── Dest:   DatabaseConfig ──► DSN()
      ├── Replication: ReplicationConfig
      ├── Snapshot:    SnapshotConfig
      └── Logging:     LoggingConfig
              │
              ▼
      Validate() → error (multi-error)
```

## Types

### `Config`

The top-level configuration struct that aggregates all settings:

```go
type Config struct {
    Source      DatabaseConfig
    Dest        DatabaseConfig
    Replication ReplicationConfig
    Snapshot    SnapshotConfig
    Logging     LoggingConfig
}
```

This is the single configuration object passed to `pipeline.New()` and shared across all components. It is populated by cobra flag binding in `cmd/migrator/root.go`.

### `DatabaseConfig`

Connection parameters for a PostgreSQL instance:

```go
type DatabaseConfig struct {
    Host     string   // Hostname or IP address
    Port     uint16   // TCP port (default: 5432)
    User     string   // PostgreSQL username (default: "postgres")
    Password string   // PostgreSQL password
    DBName   string   // Database name
}
```

#### `DSN() string`

Constructs a standard PostgreSQL connection string using `net/url`:

```go
func (d DatabaseConfig) DSN() string {
    u := url.URL{
        Scheme: "postgres",
        User:   url.UserPassword(d.User, d.Password),
        Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
        Path:   d.DBName,
    }
    return u.String()
}
```

**Output format:** `postgres://user:password@host:port/dbname`

Using `net/url.URL` ensures proper URL encoding of special characters in usernames and passwords (e.g., `@`, `:`, `/`). This is critical — unescaped passwords with special characters would corrupt the DSN string and cause connection failures.

**Examples:**
```
postgres://postgres:secret@localhost:5432/mydb
postgres://admin:p%40ssw0rd@10.0.0.1:5433/production
```

#### `ReplicationDSN() string`

Constructs a connection string with the `replication=database` query parameter:

```go
func (d DatabaseConfig) ReplicationDSN() string {
    u := url.URL{
        Scheme:   "postgres",
        User:     url.UserPassword(d.User, d.Password),
        Host:     fmt.Sprintf("%s:%d", d.Host, d.Port),
        Path:     d.DBName,
        RawQuery: "replication=database",
    }
    return u.String()
}
```

**Output format:** `postgres://user:password@host:port/dbname?replication=database`

The `replication=database` parameter tells PostgreSQL to enter the walsender protocol mode, which is required for:
- Creating replication slots (`CREATE_REPLICATION_SLOT`)
- Starting logical replication (`START_REPLICATION`)
- Receiving WAL changes (`CopyData` messages)

This DSN is used by the `stream.Decoder` when establishing the replication connection to the source database.

### `ReplicationConfig`

Settings for the WAL replication stream:

```go
type ReplicationConfig struct {
    SlotName     string   // Replication slot name (default: "migrator")
    Publication  string   // Publication name (default: "migrator_pub")
    OutputPlugin string   // Logical decoding plugin (default: "pgoutput")
    OriginID     string   // Replication origin ID for bidi (default: "" = disabled)
}
```

| Field | CLI Flag | Default | Description |
|-------|----------|---------|-------------|
| `SlotName` | `--slot` | `migrator` | Name of the logical replication slot on the source |
| `Publication` | `--publication` | `migrator_pub` | PostgreSQL publication that defines which tables to replicate |
| `OutputPlugin` | `--output-plugin` | `pgoutput` | Logical decoding output plugin. Only `pgoutput` is supported |
| `OriginID` | `--origin-id` | `""` (empty) | Replication origin name for bidirectional loop detection. When empty, bidi filtering is disabled |

#### About `pgoutput`

The `pgoutput` plugin is PostgreSQL's built-in logical decoding output plugin, available since PostgreSQL 10. It produces binary-encoded messages (RelationMessage, BeginMessage, InsertMessage, etc.) that map directly to migrator's `stream.Message` types. It's preferred over alternatives like `wal2json` because:

1. **Built-in** — No extension installation required
2. **Binary protocol** — More efficient than JSON serialization/deserialization
3. **Publication-aware** — Respects `CREATE PUBLICATION` filter rules
4. **Origin-aware** — Emits `OriginMessage` for replication origin tracking

### `SnapshotConfig`

Settings for the initial parallel COPY phase:

```go
type SnapshotConfig struct {
    Workers int   // Number of parallel COPY workers (default: 4)
}
```

| Field | CLI Flag | Default | Description |
|-------|----------|---------|-------------|
| `Workers` | `--copy-workers` | `4` | Number of concurrent goroutines copying tables from source to destination |

The worker count should be tuned based on:
- Number of CPU cores on source and destination
- Network bandwidth between source and destination
- Number of tables (more workers helps with many small tables)
- Available memory (each worker buffers one table's rows in memory)

### `LoggingConfig`

Settings for structured logging:

```go
type LoggingConfig struct {
    Level  string   // Log level: "debug", "info", "warn", "error"
    Format string   // Output format: "json" or "console"
}
```

| Field | CLI Flag | Default | Description |
|-------|----------|---------|-------------|
| `Level` | `--log-level` | `info` | Minimum log level to emit |
| `Format` | `--log-format` | `console` | `"console"` for human-readable colored output, `"json"` for structured JSON lines |

The `console` format uses `zerolog.ConsoleWriter` with RFC3339 timestamps and is best for interactive use. The `json` format produces one JSON object per line, suitable for log aggregation systems (ELK, Datadog, etc.).

## Validation

```go
func (c *Config) Validate() error
```

Checks required fields and applies defaults. Uses Go's `errors.Join()` to collect multiple validation errors into a single error value, so users see all problems at once rather than fixing them one at a time.

### Required Fields

| Field | Error Message |
|-------|---------------|
| `Source.Host` | `"source host is required"` |
| `Source.DBName` | `"source database name is required"` |
| `Dest.Host` | `"destination host is required"` |
| `Dest.DBName` | `"destination database name is required"` |
| `Replication.SlotName` | `"replication slot name is required"` |
| `Replication.Publication` | `"publication name is required"` |

### Defaults Applied

| Field | Condition | Default |
|-------|-----------|---------|
| `Replication.OutputPlugin` | Empty string | `"pgoutput"` |
| `Snapshot.Workers` | Less than 1 | `4` |

### Validation Flow

```go
if err := cfg.Validate(); err != nil {
    // err may contain multiple joined errors:
    // "source host is required\ndestination database name is required"
    return err
}
```

Validation is called at the start of every command's `RunE` function, before any connections are opened or operations begin.

## CLI Flag Binding

The config struct is populated via cobra's persistent flags in `cmd/migrator/root.go`:

```go
var cfg config.Config

func init() {
    f := rootCmd.PersistentFlags()

    // Source database
    f.StringVar(&cfg.Source.Host, "source-host", "localhost", "...")
    f.Uint16Var(&cfg.Source.Port, "source-port", 5432, "...")
    f.StringVar(&cfg.Source.User, "source-user", "postgres", "...")
    f.StringVar(&cfg.Source.Password, "source-password", "", "...")
    f.StringVar(&cfg.Source.DBName, "source-dbname", "", "...")

    // Destination database
    f.StringVar(&cfg.Dest.Host, "dest-host", "localhost", "...")
    // ... same pattern ...

    // Replication
    f.StringVar(&cfg.Replication.SlotName, "slot", "migrator", "...")
    f.StringVar(&cfg.Replication.Publication, "publication", "migrator_pub", "...")
    f.StringVar(&cfg.Replication.OutputPlugin, "output-plugin", "pgoutput", "...")
    f.StringVar(&cfg.Replication.OriginID, "origin-id", "", "...")

    // Snapshot
    f.IntVar(&cfg.Snapshot.Workers, "copy-workers", 4, "...")

    // Logging
    f.StringVar(&cfg.Logging.Level, "log-level", "info", "...")
    f.StringVar(&cfg.Logging.Format, "log-format", "console", "...")
}
```

Because these are **persistent** flags, they're available to all subcommands (clone, follow, switchover, status, etc.).

## Password Handling

Passwords are stored in plain text in the config struct (passed via CLI flags). The HTTP server's `/api/v1/config` endpoint redacts passwords before exposing the config:

```go
// In server/handlers.go:
safeCfg := *s.cfg
safeCfg.Source.Password = "***"
safeCfg.Dest.Password = "***"
```

For production deployments, passwords can also be passed via environment variables or `.pgpass` files (handled by libpq/pgx automatically when the password field is empty).

## Design Decisions

### Why Separate Fields Instead of a Single DSN?

Individual fields (host, port, user, password, dbname) rather than a single DSN string because:

1. **Validation** — Can check individual required fields and provide specific error messages
2. **Redaction** — Can redact the password without parsing a URL
3. **Flexibility** — Can construct both regular and replication DSNs from the same fields
4. **CLI UX** — Users don't have to remember URL encoding rules for special characters

### Why `errors.Join` for Validation?

Go 1.20's `errors.Join()` produces a single error containing all validation failures. This is better UX than returning on the first error, because users can fix all problems in one pass rather than playing error whack-a-mole.

### Why No Config File Support?

The current implementation uses only CLI flags. Config file support (YAML/TOML) could be added later via cobra's `viper` integration, but CLI flags are sufficient for the initial release and avoid the complexity of config file discovery, parsing, and precedence rules.
