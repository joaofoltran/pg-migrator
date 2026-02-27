package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmigrator/internal/bidi"
	"github.com/jfoltran/pgmigrator/internal/config"
	"github.com/jfoltran/pgmigrator/internal/metrics"
	"github.com/jfoltran/pgmigrator/internal/replay"
	"github.com/jfoltran/pgmigrator/internal/schema"
	"github.com/jfoltran/pgmigrator/internal/sentinel"
	"github.com/jfoltran/pgmigrator/internal/snapshot"
	"github.com/jfoltran/pgmigrator/internal/stream"
)

// Progress reports the current state of the pipeline.
type Progress struct {
	Phase        string
	LastLSN      pglogrepl.LSN
	TablesTotal  int
	TablesCopied int
	StartedAt    time.Time
}

// Pipeline orchestrates the full migration lifecycle: wires
// decoder → filter → applier, manages snapshot copies, and coordinates switchover.
type Pipeline struct {
	cfg    *config.Config
	logger zerolog.Logger

	// Connections
	replConn *pgconn.PgConn
	srcPool  *pgxpool.Pool
	dstPool  *pgxpool.Pool

	// Components
	decoder     *stream.Decoder
	applier     *replay.Applier
	copier      *snapshot.Copier
	schemaMgr   *schema.Migrator
	coordinator *sentinel.Coordinator
	bidiFilter  *bidi.Filter

	// Metrics
	Metrics   *metrics.Collector
	persister *metrics.StatePersister

	// Channel that carries messages through the pipeline.
	messages chan stream.Message

	mu       sync.Mutex
	progress Progress

	cancel context.CancelFunc
}

// New creates a new Pipeline from the given configuration.
func New(cfg *config.Config, logger zerolog.Logger) *Pipeline {
	mc := metrics.NewCollector(logger)
	return &Pipeline{
		cfg:      cfg,
		logger:   logger.With().Str("component", "pipeline").Logger(),
		messages: make(chan stream.Message, 256),
		progress: Progress{Phase: "idle"},
		Metrics:  mc,
	}
}

// connect establishes all required database connections.
func (p *Pipeline) connect(ctx context.Context) error {
	// Replication connection to source.
	replConn, err := pgconn.Connect(ctx, p.cfg.Source.ReplicationDSN())
	if err != nil {
		return fmt.Errorf("replication connection: %w", err)
	}
	p.replConn = replConn

	// Connection pool to source (for snapshot COPY).
	srcPool, err := pgxpool.New(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("source pool: %w", err)
	}
	p.srcPool = srcPool

	// Connection pool to destination.
	dstPool, err := pgxpool.New(ctx, p.cfg.Dest.DSN())
	if err != nil {
		return fmt.Errorf("dest pool: %w", err)
	}
	p.dstPool = dstPool

	return nil
}

// initComponents creates all pipeline components.
func (p *Pipeline) initComponents() {
	p.decoder = stream.NewDecoder(p.replConn, p.cfg.Replication.SlotName, p.cfg.Replication.Publication, p.logger)
	p.applier = replay.NewApplier(p.dstPool, p.logger)
	p.copier = snapshot.NewCopier(p.srcPool, p.dstPool, p.cfg.Snapshot.Workers, p.logger)
	p.schemaMgr = schema.NewMigrator(p.srcPool, p.dstPool, p.logger)
	p.coordinator = sentinel.NewCoordinator(p.messages, p.logger)

	if p.cfg.Replication.OriginID != "" {
		p.bidiFilter = bidi.NewFilter(p.cfg.Replication.OriginID, p.logger)
	}
}

// startPersister initializes state file persistence.
func (p *Pipeline) startPersister() {
	persister, err := metrics.NewStatePersister(p.Metrics, p.logger)
	if err != nil {
		p.logger.Warn().Err(err).Msg("failed to start state persister")
		return
	}
	p.persister = persister
	p.persister.Start()
}

// RunClone performs schema copy + full data copy (no CDC follow).
func (p *Pipeline) RunClone(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	// Dump and apply schema.
	p.setPhase("schema")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	if err := p.schemaMgr.ApplySchema(ctx, ddl); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	// Create replication slot to get consistent snapshot.
	msgCh, snapshotName, err := p.decoder.Start(ctx, 0)
	if err != nil {
		return fmt.Errorf("start decoder: %w", err)
	}
	// We don't consume msgCh for clone-only; drain in background.
	go func() {
		for range msgCh {
		}
	}()

	// Parallel COPY using the snapshot.
	p.setPhase("copy")
	tables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	p.mu.Lock()
	p.progress.TablesTotal = len(tables)
	p.mu.Unlock()

	// Initialize metrics table tracking.
	p.initTableMetrics(tables)

	results := p.copier.CopyAll(ctx, tables, snapshotName)
	for _, r := range results {
		if r.Err != nil {
			p.Metrics.RecordError(r.Err)
			return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
		}
		p.mu.Lock()
		p.progress.TablesCopied++
		p.mu.Unlock()
		p.Metrics.TableDone(r.Table.Schema, r.Table.Name, r.RowsCopied)
		p.Metrics.RecordApplied(0, r.RowsCopied, r.Table.SizeBytes)
	}

	p.setPhase("done")
	p.logger.Info().Msg("clone completed")
	return nil
}

// RunCloneAndFollow performs clone then transitions to CDC streaming.
func (p *Pipeline) RunCloneAndFollow(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	// Schema.
	p.setPhase("schema")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	if err := p.schemaMgr.ApplySchema(ctx, ddl); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	// Start decoder (creates slot, gets snapshot).
	msgCh, snapshotName, err := p.decoder.Start(ctx, 0)
	if err != nil {
		return fmt.Errorf("start decoder: %w", err)
	}

	// Buffer messages while COPY runs.
	buffered := make(chan stream.Message, 4096)
	go func() {
		for msg := range msgCh {
			buffered <- msg
		}
		close(buffered)
	}()

	// Parallel COPY.
	p.setPhase("copy")
	tables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	p.mu.Lock()
	p.progress.TablesTotal = len(tables)
	p.mu.Unlock()

	// Initialize metrics table tracking.
	p.initTableMetrics(tables)

	results := p.copier.CopyAll(ctx, tables, snapshotName)
	for _, r := range results {
		if r.Err != nil {
			p.Metrics.RecordError(r.Err)
			return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
		}
		p.mu.Lock()
		p.progress.TablesCopied++
		p.mu.Unlock()
		p.Metrics.TableDone(r.Table.Schema, r.Table.Name, r.RowsCopied)
		p.Metrics.RecordApplied(0, r.RowsCopied, r.Table.SizeBytes)
	}

	// Transition to CDC.
	p.setPhase("streaming")
	p.logger.Info().Msg("COPY complete, switching to CDC streaming")

	// Mark all tables as streaming.
	for _, t := range tables {
		p.Metrics.TableStreaming(t.Schema, t.Name)
	}

	var applierCh <-chan stream.Message = buffered
	if p.bidiFilter != nil {
		applierCh = p.bidiFilter.Run(ctx, buffered)
	}

	return p.applier.Start(ctx, applierCh, func(lsn pglogrepl.LSN) {
		p.decoder.ConfirmLSN(lsn)
		p.mu.Lock()
		p.progress.LastLSN = lsn
		p.mu.Unlock()
		p.Metrics.RecordApplied(lsn, 1, 0)
		p.Metrics.RecordConfirmedLSN(lsn)
	})
}

// RunFollow starts CDC streaming from the given LSN (slot must already exist).
func (p *Pipeline) RunFollow(ctx context.Context, startLSN pglogrepl.LSN) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	msgCh, _, err := p.decoder.Start(ctx, startLSN)
	if err != nil {
		return fmt.Errorf("start decoder: %w", err)
	}

	p.setPhase("streaming")

	var applierCh <-chan stream.Message = msgCh
	if p.bidiFilter != nil {
		applierCh = p.bidiFilter.Run(ctx, msgCh)
	}

	return p.applier.Start(ctx, applierCh, func(lsn pglogrepl.LSN) {
		p.decoder.ConfirmLSN(lsn)
		p.mu.Lock()
		p.progress.LastLSN = lsn
		p.mu.Unlock()
		p.Metrics.RecordApplied(lsn, 1, 0)
		p.Metrics.RecordConfirmedLSN(lsn)
	})
}

// RunSwitchover injects a sentinel message and waits for it to be confirmed,
// signaling that the destination is fully caught up.
func (p *Pipeline) RunSwitchover(ctx context.Context, timeout time.Duration) error {
	if p.coordinator == nil {
		return fmt.Errorf("pipeline not initialized")
	}

	p.setPhase("switchover")
	currentLSN := p.applier.LastLSN()

	id, err := p.coordinator.Initiate(ctx, currentLSN)
	if err != nil {
		return fmt.Errorf("initiate sentinel: %w", err)
	}

	if err := p.coordinator.WaitForConfirmation(id, timeout); err != nil {
		return fmt.Errorf("switchover: %w", err)
	}

	p.setPhase("switchover-complete")
	p.logger.Info().Msg("switchover confirmed — destination is caught up")
	return nil
}

// Status returns a snapshot of the current pipeline progress.
func (p *Pipeline) Status() Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.progress
}

// Close shuts down all pipeline components and connections.
func (p *Pipeline) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.Metrics != nil {
		p.Metrics.Close()
	}
	if p.persister != nil {
		p.persister.Stop()
	}
	if p.decoder != nil {
		p.decoder.Close()
	}
	if p.applier != nil {
		p.applier.Close()
	}
	if p.replConn != nil {
		p.replConn.Close(context.Background()) //nolint:errcheck
	}
	if p.srcPool != nil {
		p.srcPool.Close()
	}
	if p.dstPool != nil {
		p.dstPool.Close()
	}
}

func (p *Pipeline) setPhase(phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.progress.Phase = phase
	if p.progress.StartedAt.IsZero() {
		p.progress.StartedAt = time.Now()
	}
	p.logger.Info().Str("phase", phase).Msg("phase transition")
	p.Metrics.SetPhase(phase)
}

func (p *Pipeline) initTableMetrics(tables []snapshot.TableInfo) {
	tps := make([]metrics.TableProgress, len(tables))
	for i, t := range tables {
		tps[i] = metrics.TableProgress{
			Schema:    t.Schema,
			Name:      t.Name,
			Status:    metrics.TablePending,
			RowsTotal: t.RowCount,
			SizeBytes: t.SizeBytes,
		}
	}
	p.Metrics.SetTables(tps)
}

// Config returns the pipeline configuration (for API exposure).
func (p *Pipeline) Config() *config.Config {
	return p.cfg
}
