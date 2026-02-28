# Schema Management

**Package:** `internal/migration/schema`
**File:** `schema.go`

## Overview

The schema package handles DDL (Data Definition Language) operations between source and destination databases. It provides three capabilities: dumping the source schema, applying it to the destination, and comparing schemas to detect drift. This is always the first step in a migration — the destination must have the correct table structures before data can be copied.

## Architecture

```
Source PG ──(pg_dump)──► DDL string ──(Exec)──► Destination PG

Source PG ──(pg_tables + information_schema)──► Schema comparison
Dest PG   ──(pg_tables + information_schema)──┘
```

## Types

### `Migrator`

```go
type Migrator struct {
    source *pgxpool.Pool   // Source connection pool
    dest   *pgxpool.Pool   // Destination connection pool
    logger zerolog.Logger   // Component-tagged logger
}
```

### `SchemaDiff`

Represents differences between source and destination schemas:

```go
type SchemaDiff struct {
    MissingTables []string      // Tables in source but not destination
    ExtraTables   []string      // Tables in destination but not source
    ColumnDiffs   []ColumnDiff  // Column type mismatches
}
```

**`HasDifferences() bool`** — Returns true if any differences were found.

### `ColumnDiff`

Describes a column mismatch:

```go
type ColumnDiff struct {
    Table      string  // Qualified table name (schema.table)
    Column     string  // Column name
    SourceType string  // Data type in source
    DestType   string  // Data type in destination (or "(missing)")
}
```

## Construction

```go
migrator := schema.NewMigrator(srcPool, dstPool, logger)
```

## Schema Dump

```go
func (m *Migrator) DumpSchema(ctx context.Context, dsn string) (string, error)
```

Runs `pg_dump` as an external process to extract the complete DDL:

```bash
pg_dump --schema-only --no-owner --no-privileges <dsn>
```

| Flag | Purpose |
|------|---------|
| `--schema-only` | Only DDL, no data |
| `--no-owner` | Omit `ALTER ... OWNER TO` statements |
| `--no-privileges` | Omit `GRANT`/`REVOKE` statements |

**Why `pg_dump`?** It's the only reliable way to get a complete DDL dump that includes:
- Table definitions with all column types, defaults, constraints
- Indexes (unique, B-tree, GIN, GiST, etc.)
- Sequences and their ownership
- Foreign key constraints
- Check constraints
- Triggers
- Views and materialized views
- Extensions
- Custom types and domains
- Partitioning schemes

Reconstructing all of this from `information_schema` queries would be fragile and incomplete.

**Error handling:** If `pg_dump` exits non-zero, the stderr output is included in the error message for debugging.

**Requirement:** `pg_dump` must be available in the system `PATH`. The Docker image includes `postgresql-client` for this reason.

## Schema Apply

```go
func (m *Migrator) ApplySchema(ctx context.Context, ddl string) error
```

Executes the DDL string against the destination database as a single `Exec` call. PostgreSQL processes the DDL statements sequentially within the connection.

This is a straightforward operation — the DDL from `pg_dump` is designed to be replayed on a fresh database. The `--no-owner` and `--no-privileges` flags ensure it works even when the destination user doesn't have superuser privileges.

## Schema Comparison

```go
func (m *Migrator) CompareSchemas(ctx context.Context) (*SchemaDiff, error)
```

Compares user table structures between source and destination databases. This is used for validation — confirming that the schema was applied correctly or detecting drift after extended streaming.

### Comparison Algorithm

1. **List user tables** from both databases:
   ```sql
   SELECT schemaname || '.' || tablename
   FROM pg_tables
   WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
   ORDER BY schemaname, tablename
   ```

2. **Find missing/extra tables** by set difference:
   - Tables in source but not destination → `MissingTables`
   - Tables in destination but not source → `ExtraTables`

3. **Compare columns** for tables present in both:
   ```sql
   SELECT column_name, data_type
   FROM information_schema.columns
   WHERE table_schema = $1 AND table_name = $2
   ORDER BY ordinal_position
   ```

   For each table:
   - Build a map of destination columns (`name → data_type`)
   - Iterate source columns:
     - If column exists in destination but type differs → `ColumnDiff`
     - If column doesn't exist in destination → `ColumnDiff` with `DestType: "(missing)"`

### Limitations

The current comparison checks:
- Table existence
- Column names and data types

It does **not** check:
- Index definitions
- Constraint definitions
- Default values
- Sequence ownership
- Trigger definitions
- View definitions

These could be added in the future by extending the comparison queries.

## Internal Helpers

### `listUserTables(ctx, pool) ([]string, error)`

Returns all user table names as `schema.table` strings, excluding system schemas (`pg_catalog`, `information_schema`).

### `listColumns(ctx, pool, qualifiedTable) ([]colInfo, error)`

Returns column metadata for a single table. The `colInfo` struct holds `name` and `dataType` fields. The qualified table name is split on `.` to extract schema and table name for the `information_schema.columns` query.

## Usage in Pipeline

The schema operations run during the `schema` phase of the pipeline:

```go
// In Pipeline.RunClone() and RunCloneAndFollow():
p.setPhase("schema")
ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
if err != nil {
    return fmt.Errorf("dump schema: %w", err)
}
if err := p.schemaMgr.ApplySchema(ctx, ddl); err != nil {
    return fmt.Errorf("apply schema: %w", err)
}
```

The `compare` CLI command (currently a stub) will use `CompareSchemas()` to validate schema consistency.
