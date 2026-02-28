package snapshot

import (
	"context"
	"fmt"
	"strings"
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

// ProgressFunc is called to report COPY progress for a table.
// event is "start", "progress", or "done".
type ProgressFunc func(table TableInfo, event string, rowsCopied int64)

// Copier performs parallel COPY of tables using a consistent snapshot.
type Copier struct {
	source   *pgxpool.Pool
	dest     *pgxpool.Pool
	logger   zerolog.Logger
	progress ProgressFunc

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

// SetProgressFunc sets a callback for COPY progress reporting.
func (c *Copier) SetProgressFunc(fn ProgressFunc) {
	c.progress = fn
}

// ListTables returns all user tables from the source database.
func (c *Copier) ListTables(ctx context.Context) ([]TableInfo, error) {
	rows, err := c.source.Query(ctx, `
		SELECT schemaname, relname,
			COALESCE(n_live_tup, 0),
			COALESCE(pg_table_size(schemaname || '.' || relname), 0)
		FROM pg_stat_user_tables
		ORDER BY pg_table_size(schemaname || '.' || relname) DESC`)
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

// DestRowCount returns the exact row count for a table on the destination.
func (c *Copier) DestRowCount(ctx context.Context, schema, name string) (int64, error) {
	qn := quoteQualifiedName(schema, name)
	var count int64
	err := c.dest.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", qn)).Scan(&count)
	return count, err
}

// TruncateTable truncates a table on the destination.
func (c *Copier) TruncateTable(ctx context.Context, schema, name string) error {
	qn := quoteQualifiedName(schema, name)
	_, err := c.dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", qn))
	return err
}

// DestHasData returns true if any of the given tables have rows on the destination.
func (c *Copier) DestHasData(ctx context.Context, tables []TableInfo) (bool, error) {
	for _, t := range tables {
		qn := quoteQualifiedName(t.Schema, t.Name)
		var exists bool
		err := c.dest.QueryRow(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s LIMIT 1)", qn)).Scan(&exists)
		if err != nil {
			return false, fmt.Errorf("check %s: %w", qn, err)
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
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

func (c *Copier) reportProgress(table TableInfo, event string, rowsCopied int64) {
	if c.progress != nil {
		c.progress(table, event, rowsCopied)
	}
}

const copyBatchSize = 50000

func (c *Copier) copyTable(ctx context.Context, table TableInfo, snapshotName string, workerID int) CopyResult {
	log := c.logger.With().Str("table", table.QualifiedName()).Int("worker", workerID).Logger()
	log.Info().Msg("starting COPY")
	c.reportProgress(table, "start", 0)

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

	qn := quoteQualifiedName(table.Schema, table.Name)
	rows, err := srcTx.Query(ctx, fmt.Sprintf("SELECT * FROM %s", qn))
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("select from %s: %w", qn, err)}
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = fd.Name
	}

	var totalCopied int64
	batch := make([][]any, 0, copyBatchSize)

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return CopyResult{Table: table, Err: fmt.Errorf("read row: %w", err)}
		}
		batch = append(batch, vals)

		if len(batch) >= copyBatchSize {
			n, err := c.dest.CopyFrom(ctx,
				pgx.Identifier{table.Schema, table.Name},
				colNames,
				pgx.CopyFromRows(batch))
			if err != nil {
				return CopyResult{Table: table, Err: fmt.Errorf("copy to %s: %w", qn, err)}
			}
			totalCopied += n
			batch = batch[:0]
			c.reportProgress(table, "progress", totalCopied)
		}
	}
	if err := rows.Err(); err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("rows iteration: %w", err)}
	}

	if len(batch) > 0 {
		n, err := c.dest.CopyFrom(ctx,
			pgx.Identifier{table.Schema, table.Name},
			colNames,
			pgx.CopyFromRows(batch))
		if err != nil {
			return CopyResult{Table: table, Err: fmt.Errorf("copy to %s: %w", qn, err)}
		}
		totalCopied += n
	}

	log.Info().Int64("rows", totalCopied).Msg("COPY complete")
	c.reportProgress(table, "done", totalCopied)
	return CopyResult{Table: table, RowsCopied: totalCopied}
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteQualifiedName(schema, table string) string {
	if schema == "" || schema == "public" {
		return quoteIdent(table)
	}
	return quoteIdent(schema) + "." + quoteIdent(table)
}
