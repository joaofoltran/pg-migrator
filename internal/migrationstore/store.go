package migrationstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Mode string

const (
	ModeCloneOnly       Mode = "clone_only"
	ModeCloneAndFollow  Mode = "clone_and_follow"
	ModeCloneFollowSwitch Mode = "clone_follow_switchover"
)

type Status string

const (
	StatusCreated    Status = "created"
	StatusRunning    Status = "running"
	StatusStreaming  Status = "streaming"
	StatusSwitchover Status = "switchover"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusStopped    Status = "stopped"
)

type Migration struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	SourceClusterID string     `json:"source_cluster_id"`
	DestClusterID   string     `json:"dest_cluster_id"`
	SourceNodeID    string     `json:"source_node_id"`
	DestNodeID      string     `json:"dest_node_id"`
	Mode            Mode       `json:"mode"`
	Fallback        bool       `json:"fallback"`
	Status          Status     `json:"status"`
	Phase           string     `json:"phase"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	SlotName        string     `json:"slot_name"`
	Publication     string     `json:"publication"`
	CopyWorkers     int        `json:"copy_workers"`
	ConfirmedLSN    string     `json:"confirmed_lsn,omitempty"`
	TablesTotal     int        `json:"tables_total"`
	TablesCopied    int        `json:"tables_copied"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) List(ctx context.Context) ([]Migration, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, source_cluster_id, dest_cluster_id, source_node_id, dest_node_id,
		       mode, fallback, status, phase, error_message, slot_name, publication, copy_workers,
		       confirmed_lsn, tables_total, tables_copied,
		       started_at, finished_at, created_at, updated_at
		FROM migrations ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	defer rows.Close()

	var list []Migration
	for rows.Next() {
		m, err := scanMigration(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	if list == nil {
		list = []Migration{}
	}
	return list, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (Migration, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, source_cluster_id, dest_cluster_id, source_node_id, dest_node_id,
		       mode, fallback, status, phase, error_message, slot_name, publication, copy_workers,
		       confirmed_lsn, tables_total, tables_copied,
		       started_at, finished_at, created_at, updated_at
		FROM migrations WHERE id = $1
	`, id)
	if err != nil {
		return Migration{}, false, fmt.Errorf("get migration: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return Migration{}, false, nil
	}
	m, err := scanMigration(rows)
	if err != nil {
		return Migration{}, false, err
	}
	return m, true, nil
}

func (s *Store) Create(ctx context.Context, m Migration) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO migrations (id, name, source_cluster_id, dest_cluster_id, source_node_id, dest_node_id,
		                        mode, fallback, status, slot_name, publication, copy_workers)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, m.ID, m.Name, m.SourceClusterID, m.DestClusterID, m.SourceNodeID, m.DestNodeID,
		m.Mode, m.Fallback, StatusCreated, m.SlotName, m.Publication, m.CopyWorkers)
	if err != nil {
		return fmt.Errorf("create migration: %w", err)
	}
	return nil
}

func (s *Store) UpdateStatus(ctx context.Context, id string, status Status, phase string, errMsg string) error {
	now := time.Now()
	var startedAt, finishedAt *time.Time

	switch status {
	case StatusRunning:
		startedAt = &now
	case StatusCompleted, StatusFailed, StatusStopped:
		finishedAt = &now
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE migrations SET
			status = $2, phase = $3, error_message = $4, updated_at = now(),
			started_at = COALESCE($5, started_at),
			finished_at = COALESCE($6, finished_at)
		WHERE id = $1
	`, id, status, phase, errMsg, startedAt, finishedAt)
	if err != nil {
		return fmt.Errorf("update migration status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("migration not found")
	}
	return nil
}

func (s *Store) UpdateProgress(ctx context.Context, id string, phase string, lsn string, tablesTotal, tablesCopied int) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE migrations SET
			phase = $2, confirmed_lsn = $3, tables_total = $4, tables_copied = $5, updated_at = now()
		WHERE id = $1
	`, id, phase, lsn, tablesTotal, tablesCopied)
	if err != nil {
		return fmt.Errorf("update migration progress: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("migration not found")
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM migrations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete migration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("migration not found")
	}
	return nil
}

func scanMigration(rows pgx.Rows) (Migration, error) {
	var m Migration
	err := rows.Scan(
		&m.ID, &m.Name, &m.SourceClusterID, &m.DestClusterID, &m.SourceNodeID, &m.DestNodeID,
		&m.Mode, &m.Fallback, &m.Status, &m.Phase, &m.ErrorMessage, &m.SlotName, &m.Publication, &m.CopyWorkers,
		&m.ConfirmedLSN, &m.TablesTotal, &m.TablesCopied,
		&m.StartedAt, &m.FinishedAt, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return Migration{}, fmt.Errorf("scan migration: %w", err)
	}
	return m, nil
}

func ValidateMigration(m Migration) error {
	var errs []error
	if m.ID == "" {
		errs = append(errs, errors.New("migration id is required"))
	}
	if m.Name == "" {
		errs = append(errs, errors.New("migration name is required"))
	}
	if m.SourceClusterID == "" {
		errs = append(errs, errors.New("source cluster is required"))
	}
	if m.DestClusterID == "" {
		errs = append(errs, errors.New("destination cluster is required"))
	}
	if m.SourceClusterID == m.DestClusterID && m.SourceNodeID == m.DestNodeID {
		errs = append(errs, errors.New("source and destination cannot be the same node"))
	}
	if m.SourceNodeID == "" {
		errs = append(errs, errors.New("source node is required"))
	}
	if m.DestNodeID == "" {
		errs = append(errs, errors.New("destination node is required"))
	}
	switch m.Mode {
	case ModeCloneOnly, ModeCloneAndFollow, ModeCloneFollowSwitch:
	default:
		errs = append(errs, fmt.Errorf("invalid mode %q", m.Mode))
	}
	return errors.Join(errs...)
}
