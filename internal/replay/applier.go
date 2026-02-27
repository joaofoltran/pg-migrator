package replay

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmigrator/internal/stream"
)

// Applier reads Messages from a channel and applies DML to the destination.
type Applier struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger

	mu      sync.Mutex
	lastLSN pglogrepl.LSN

	// relations caches relation metadata keyed by relation ID.
	relations map[uint32]*stream.RelationMessage
}

// NewApplier creates an Applier that writes to the given connection pool.
func NewApplier(pool *pgxpool.Pool, logger zerolog.Logger) *Applier {
	return &Applier{
		pool:      pool,
		logger:    logger.With().Str("component", "applier").Logger(),
		relations: make(map[uint32]*stream.RelationMessage),
	}
}

// OnApplied is a callback invoked after a commit message has been applied.
type OnApplied func(lsn pglogrepl.LSN)

// Start consumes messages and applies them to the destination database.
// It blocks until the input channel is closed or the context is cancelled.
func (a *Applier) Start(ctx context.Context, messages <-chan stream.Message, onApplied OnApplied) error {
	var tx pgx.Tx

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				return nil
			}

			switch m := msg.(type) {
			case *stream.RelationMessage:
				a.relations[m.RelationID] = m

			case *stream.BeginMessage:
				var err error
				tx, err = a.pool.Begin(ctx)
				if err != nil {
					return fmt.Errorf("begin tx: %w", err)
				}

			case *stream.ChangeMessage:
				if tx == nil {
					a.logger.Warn().Msg("change outside transaction, skipping")
					continue
				}
				var err error
				switch m.Op {
				case stream.OpInsert:
					err = a.applyInsert(ctx, tx, m)
				case stream.OpUpdate:
					err = a.applyUpdate(ctx, tx, m)
				case stream.OpDelete:
					err = a.applyDelete(ctx, tx, m)
				}
				if err != nil {
					_ = tx.Rollback(ctx)
					tx = nil
					return fmt.Errorf("apply %s on %s.%s: %w", m.Op, m.Namespace, m.Table, err)
				}

			case *stream.CommitMessage:
				if tx != nil {
					if err := tx.Commit(ctx); err != nil {
						return fmt.Errorf("commit tx: %w", err)
					}
					tx = nil
				}
				a.mu.Lock()
				a.lastLSN = m.CommitLSN
				a.mu.Unlock()
				if onApplied != nil {
					onApplied(m.CommitLSN)
				}
			}
		}
	}
}

func (a *Applier) applyInsert(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error {
	if m.NewTuple == nil {
		return nil
	}
	cols, vals, placeholders := a.buildInsertParts(m.NewTuple)
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		qualifiedName(m.Namespace, m.Table),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "))

	_, err := tx.Exec(ctx, query, vals...)
	return err
}

func (a *Applier) applyUpdate(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error {
	if m.NewTuple == nil {
		return nil
	}

	rel := a.relations[m.RelationID]
	setClauses, setVals := a.buildSetClauses(m.NewTuple)
	whereClauses, whereVals := a.buildWhereClauses(m, rel, len(setVals))

	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		qualifiedName(m.Namespace, m.Table),
		strings.Join(setClauses, ", "),
		strings.Join(whereClauses, " AND "))

	allVals := append(setVals, whereVals...)
	_, err := tx.Exec(ctx, query, allVals...)
	return err
}

func (a *Applier) applyDelete(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error {
	rel := a.relations[m.RelationID]
	whereClauses, whereVals := a.buildWhereClauses(m, rel, 0)

	query := fmt.Sprintf("DELETE FROM %s WHERE %s",
		qualifiedName(m.Namespace, m.Table),
		strings.Join(whereClauses, " AND "))

	_, err := tx.Exec(ctx, query, whereVals...)
	return err
}

func (a *Applier) buildInsertParts(tuple *stream.TupleData) (cols []string, vals []any, placeholders []string) {
	for i, c := range tuple.Columns {
		cols = append(cols, quoteIdent(c.Name))
		vals = append(vals, string(c.Value))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	return
}

func (a *Applier) buildSetClauses(tuple *stream.TupleData) (clauses []string, vals []any) {
	for i, c := range tuple.Columns {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", quoteIdent(c.Name), i+1))
		vals = append(vals, string(c.Value))
	}
	return
}

func (a *Applier) buildWhereClauses(m *stream.ChangeMessage, rel *stream.RelationMessage, offset int) (clauses []string, vals []any) {
	// Prefer OldTuple (replica identity) for WHERE; fall back to NewTuple.
	source := m.OldTuple
	if source == nil {
		source = m.NewTuple
	}
	if source == nil {
		return
	}
	for i, c := range source.Columns {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", quoteIdent(c.Name), offset+i+1))
		vals = append(vals, string(c.Value))
	}
	return
}

// LastLSN returns the LSN of the most recently committed transaction.
func (a *Applier) LastLSN() pglogrepl.LSN {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastLSN
}

// Close releases resources held by the Applier.
func (a *Applier) Close() {
	// Pool is managed externally.
}

func qualifiedName(namespace, table string) string {
	if namespace == "" || namespace == "public" {
		return quoteIdent(table)
	}
	return quoteIdent(namespace) + "." + quoteIdent(table)
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
