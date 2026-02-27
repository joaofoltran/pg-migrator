package snapshot

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// TableInfo describes a table eligible for COPY.
type TableInfo struct {
	Schema    string
	Name      string
	RowCount  int64
	SizeBytes int64
}

// QualifiedName returns schema.table.
func (t TableInfo) QualifiedName() string {
	if t.Schema == "" || t.Schema == "public" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// CopyResult holds the outcome of copying a single table.
type CopyResult struct {
	Table    TableInfo
	RowsCopied int64
	Err      error
}

// Copier performs parallel COPY of tables using a consistent snapshot.
type Copier struct {
	source *pgxpool.Pool
	dest   *pgxpool.Pool
	logger zerolog.Logger

	workers int
}

// NewCopier creates a Copier with the given source/dest pools and worker count.
func NewCopier(source, dest *pgxpool.Pool, workers int, logger zerolog.Logger) *Copier {
	return &Copier{
		source:  source,
		dest:    dest,
		logger:  logger.With().Str("component", "snapshot").Logger(),
		workers: workers,
	}
}

// ListTables returns all user tables from the source database.
func (c *Copier) ListTables(ctx context.Context) ([]TableInfo, error) {
	rows, err := c.source.Query(ctx, `
		SELECT schemaname, tablename,
			COALESCE(n_live_tup, 0),
			COALESCE(pg_table_size(schemaname || '.' || tablename), 0)
		FROM pg_stat_user_tables
		ORDER BY pg_table_size(schemaname || '.' || tablename) DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.RowCount, &t.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan table info: %w", err)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// CopyAll copies all given tables in parallel using the provided snapshot name
// for read consistency. It returns results for each table.
func (c *Copier) CopyAll(ctx context.Context, tables []TableInfo, snapshotName string) []CopyResult {
	work := make(chan TableInfo, len(tables))
	for _, t := range tables {
		work <- t
	}
	close(work)

	var (
		mu      sync.Mutex
		results []CopyResult
		wg      sync.WaitGroup
	)

	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for t := range work {
				result := c.copyTable(ctx, t, snapshotName, workerID)
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return results
}

func (c *Copier) copyTable(ctx context.Context, table TableInfo, snapshotName string, workerID int) CopyResult {
	log := c.logger.With().Str("table", table.QualifiedName()).Int("worker", workerID).Logger()
	log.Info().Msg("starting COPY")

	// Acquire a source connection and set the snapshot.
	srcConn, err := c.source.Acquire(ctx)
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("acquire source conn: %w", err)}
	}
	defer srcConn.Release()

	srcTx, err := srcConn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("begin source tx: %w", err)}
	}
	defer srcTx.Rollback(ctx) //nolint:errcheck

	if snapshotName != "" {
		if _, err := srcTx.Exec(ctx, fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", snapshotName)); err != nil {
			return CopyResult{Table: table, Err: fmt.Errorf("set snapshot: %w", err)}
		}
	}

	// Read all rows from source.
	qn := table.QualifiedName()
	rows, err := srcTx.Query(ctx, fmt.Sprintf("SELECT * FROM %s", qn))
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("select from %s: %w", qn, err)}
	}
	defer rows.Close()

	// Build column list from field descriptions.
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = fd.Name
	}

	// Collect rows for COPY IN.
	var copyRows [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return CopyResult{Table: table, Err: fmt.Errorf("read row: %w", err)}
		}
		copyRows = append(copyRows, vals)
	}
	if err := rows.Err(); err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("rows iteration: %w", err)}
	}

	// Write rows to destination via COPY.
	count, err := c.dest.CopyFrom(ctx,
		pgx.Identifier{table.Schema, table.Name},
		colNames,
		pgx.CopyFromRows(copyRows))
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("copy to %s: %w", qn, err)}
	}

	log.Info().Int64("rows", count).Msg("COPY complete")
	return CopyResult{Table: table, RowsCopied: count}
}
