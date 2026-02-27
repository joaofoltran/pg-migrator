package schema

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// Migrator handles DDL operations between source and destination databases.
type Migrator struct {
	source *pgxpool.Pool
	dest   *pgxpool.Pool
	logger zerolog.Logger
}

// NewMigrator creates a schema Migrator.
func NewMigrator(source, dest *pgxpool.Pool, logger zerolog.Logger) *Migrator {
	return &Migrator{
		source: source,
		dest:   dest,
		logger: logger.With().Str("component", "schema").Logger(),
	}
}

// DumpSchema returns the DDL for the source database using pg_dump --schema-only.
func (m *Migrator) DumpSchema(ctx context.Context, dsn string) (string, error) {
	cmd := exec.CommandContext(ctx, "pg_dump", "--schema-only", "--no-owner", "--no-privileges", dsn)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("pg_dump failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("pg_dump: %w", err)
	}
	return string(out), nil
}

// ApplySchema applies the given DDL to the destination database.
func (m *Migrator) ApplySchema(ctx context.Context, ddl string) error {
	_, err := m.dest.Exec(ctx, ddl)
	if err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	m.logger.Info().Msg("schema applied to destination")
	return nil
}

// SchemaDiff represents differences between source and destination schemas.
type SchemaDiff struct {
	MissingTables []string
	ExtraTables   []string
	ColumnDiffs   []ColumnDiff
}

// ColumnDiff describes a column mismatch between source and destination.
type ColumnDiff struct {
	Table      string
	Column     string
	SourceType string
	DestType   string
}

// HasDifferences returns true if any schema differences were found.
func (d *SchemaDiff) HasDifferences() bool {
	return len(d.MissingTables) > 0 || len(d.ExtraTables) > 0 || len(d.ColumnDiffs) > 0
}

// CompareSchemas compares user table structures between source and destination.
func (m *Migrator) CompareSchemas(ctx context.Context) (*SchemaDiff, error) {
	srcTables, err := m.listUserTables(ctx, m.source)
	if err != nil {
		return nil, fmt.Errorf("list source tables: %w", err)
	}
	destTables, err := m.listUserTables(ctx, m.dest)
	if err != nil {
		return nil, fmt.Errorf("list dest tables: %w", err)
	}

	diff := &SchemaDiff{}

	srcSet := make(map[string]bool)
	for _, t := range srcTables {
		srcSet[t] = true
	}
	destSet := make(map[string]bool)
	for _, t := range destTables {
		destSet[t] = true
	}

	for _, t := range srcTables {
		if !destSet[t] {
			diff.MissingTables = append(diff.MissingTables, t)
		}
	}
	for _, t := range destTables {
		if !srcSet[t] {
			diff.ExtraTables = append(diff.ExtraTables, t)
		}
	}

	// Compare columns for tables present in both.
	for _, t := range srcTables {
		if !destSet[t] {
			continue
		}
		srcCols, err := m.listColumns(ctx, m.source, t)
		if err != nil {
			return nil, fmt.Errorf("list source columns for %s: %w", t, err)
		}
		destCols, err := m.listColumns(ctx, m.dest, t)
		if err != nil {
			return nil, fmt.Errorf("list dest columns for %s: %w", t, err)
		}
		destColMap := make(map[string]string)
		for _, c := range destCols {
			destColMap[c.name] = c.dataType
		}
		for _, c := range srcCols {
			if dt, ok := destColMap[c.name]; ok {
				if dt != c.dataType {
					diff.ColumnDiffs = append(diff.ColumnDiffs, ColumnDiff{
						Table:      t,
						Column:     c.name,
						SourceType: c.dataType,
						DestType:   dt,
					})
				}
			} else {
				diff.ColumnDiffs = append(diff.ColumnDiffs, ColumnDiff{
					Table:      t,
					Column:     c.name,
					SourceType: c.dataType,
					DestType:   "(missing)",
				})
			}
		}
	}

	return diff, nil
}

type colInfo struct {
	name     string
	dataType string
}

func (m *Migrator) listUserTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT schemaname || '.' || tablename
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schemaname, tablename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (m *Migrator) listColumns(ctx context.Context, pool *pgxpool.Pool, qualifiedTable string) ([]colInfo, error) {
	parts := strings.SplitN(qualifiedTable, ".", 2)
	schema, table := parts[0], parts[1]

	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []colInfo
	for rows.Next() {
		var c colInfo
		if err := rows.Scan(&c.name, &c.dataType); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}
