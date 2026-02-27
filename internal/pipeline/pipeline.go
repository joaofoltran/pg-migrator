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

// SetLogger replaces the pipeline logger. Use this to redirect log output
// (e.g. into the TUI metrics collector instead of stderr).
func (p *Pipeline) SetLogger(logger zerolog.Logger) {
	p.logger = logger.With().Str("component", "pipeline").Logger()
}

// connect establishes all required database connections.
func (p *Pipeline) connect(ctx context.Context) error {
	connTimeout := 30 * time.Second

	p.logger.Info().Str("host", p.cfg.Source.Host).Uint16("port", p.cfg.Source.Port).Str("db", p.cfg.Source.DBName).Msg("connecting to source (replication)")
	replCtx, replCancel := context.WithTimeout(ctx, connTimeout)
	replConn, err := pgconn.Connect(replCtx, p.cfg.Source.ReplicationDSN())
	replCancel()
	if err != nil {
		return fmt.Errorf("replication connection to %s:%d/%s: %w", p.cfg.Source.Host, p.cfg.Source.Port, p.cfg.Source.DBName, err)
	}
	p.replConn = replConn

	p.logger.Info().Str("host", p.cfg.Source.Host).Uint16("port", p.cfg.Source.Port).Str("db", p.cfg.Source.DBName).Msg("connecting to source (pool)")
	srcPool, err := pgxpool.New(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("source pool: %w", err)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, connTimeout)
	if err := srcPool.Ping(pingCtx); err != nil {
		pingCancel()
		srcPool.Close()
		return fmt.Errorf("source pool ping %s:%d/%s: %w", p.cfg.Source.Host, p.cfg.Source.Port, p.cfg.Source.DBName, err)
	}
	pingCancel()
	p.srcPool = srcPool

	p.logger.Info().Str("host", p.cfg.Dest.Host).Uint16("port", p.cfg.Dest.Port).Str("db", p.cfg.Dest.DBName).Msg("connecting to destination (pool)")
	dstPool, err := pgxpool.New(ctx, p.cfg.Dest.DSN())
	if err != nil {
		return fmt.Errorf("dest pool: %w", err)
	}
	pingCtx2, pingCancel2 := context.WithTimeout(ctx, connTimeout)
	if err := dstPool.Ping(pingCtx2); err != nil {
		pingCancel2()
		dstPool.Close()
		return fmt.Errorf("dest pool ping %s:%d/%s: %w", p.cfg.Dest.Host, p.cfg.Dest.Port, p.cfg.Dest.DBName, err)
	}
	pingCancel2()
	p.dstPool = dstPool

	p.logger.Info().Msg("all connections established")
	return nil
}

// initComponents creates all pipeline components.
func (p *Pipeline) initComponents() {
	p.decoder = stream.NewDecoder(p.replConn, p.cfg.Replication.SlotName, p.cfg.Replication.Publication, p.logger)
	p.applier = replay.NewApplier(p.dstPool, p.logger)
	p.copier = snapshot.NewCopier(p.srcPool, p.dstPool, p.cfg.Snapshot.Workers, p.logger)
	lastReported := &sync.Map{}
	p.copier.SetProgressFunc(func(table snapshot.TableInfo, event string, rowsCopied int64) {
		key := table.Schema + "." + table.Name
		switch event {
		case "start":
			lastReported.Store(key, int64(0))
			p.Metrics.TableStarted(table.Schema, table.Name)
		case "progress":
			var delta int64
			if prev, ok := lastReported.Load(key); ok {
				delta = rowsCopied - prev.(int64)
			} else {
				delta = rowsCopied
			}
			lastReported.Store(key, rowsCopied)
			p.Metrics.UpdateTableProgress(table.Schema, table.Name, rowsCopied, 0)
			p.Metrics.RecordApplied(0, delta, 0)
		case "done":
			var delta int64
			if prev, ok := lastReported.Load(key); ok {
				delta = rowsCopied - prev.(int64)
			}
			if delta > 0 {
				p.Metrics.RecordApplied(0, delta, 0)
			}
			p.Metrics.TableDone(table.Schema, table.Name, rowsCopied)
			p.mu.Lock()
			p.progress.TablesCopied++
			p.mu.Unlock()
		}
	})
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
	p.logger.Info().Msg("dumping schema from source")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	p.logger.Info().Msg("applying schema to destination")
	if err := p.schemaMgr.ApplySchema(ctx, ddl); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	// Create replication slot to get consistent snapshot.
	// The snapshot stays valid until StartStreaming is called.
	p.logger.Info().Str("slot", p.cfg.Replication.SlotName).Msg("creating replication slot")
	snapshotName, err := p.decoder.CreateSlot(ctx, 0)
	if err != nil {
		return fmt.Errorf("create slot: %w", err)
	}
	p.logger.Info().Str("snapshot", snapshotName).Msg("replication slot created")

	// Parallel COPY using the snapshot (must complete before StartStreaming).
	p.setPhase("copy")
	tables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	p.logger.Info().Int("tables", len(tables)).Int("workers", p.cfg.Snapshot.Workers).Msg("starting parallel COPY")

	p.mu.Lock()
	p.progress.TablesTotal = len(tables)
	p.mu.Unlock()

	p.initTableMetrics(tables)

	results := p.copier.CopyAll(ctx, tables, snapshotName)
	for _, r := range results {
		if r.Err != nil {
			p.Metrics.RecordError(r.Err)
			return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
		}
		p.Metrics.RecordApplied(0, 0, r.Table.SizeBytes)
	}

	// Start and immediately drain the replication stream (clone-only, no CDC).
	msgCh, err := p.decoder.StartStreaming(ctx)
	if err != nil {
		return fmt.Errorf("start streaming: %w", err)
	}
	go func() {
		for range msgCh {
		}
	}()

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
	p.logger.Info().Msg("dumping schema from source")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	p.logger.Info().Msg("applying schema to destination")
	if err := p.schemaMgr.ApplySchema(ctx, ddl); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	// Create replication slot to get consistent snapshot.
	p.logger.Info().Str("slot", p.cfg.Replication.SlotName).Msg("creating replication slot")
	snapshotName, err := p.decoder.CreateSlot(ctx, 0)
	if err != nil {
		return fmt.Errorf("create slot: %w", err)
	}
	p.logger.Info().Str("snapshot", snapshotName).Msg("replication slot created")

	// Parallel COPY using the snapshot (must complete before StartStreaming).
	p.setPhase("copy")
	tables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	p.logger.Info().Int("tables", len(tables)).Int("workers", p.cfg.Snapshot.Workers).Msg("starting parallel COPY")

	p.mu.Lock()
	p.progress.TablesTotal = len(tables)
	p.mu.Unlock()

	p.initTableMetrics(tables)

	results := p.copier.CopyAll(ctx, tables, snapshotName)
	for _, r := range results {
		if r.Err != nil {
			p.Metrics.RecordError(r.Err)
			return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
		}
		p.Metrics.RecordApplied(0, 0, r.Table.SizeBytes)
	}

	// COPY complete — now start streaming. This invalidates the snapshot
	// but we no longer need it. WAL accumulated since the slot was created
	// will be delivered through the channel.
	msgCh, err := p.decoder.StartStreaming(ctx)
	if err != nil {
		return fmt.Errorf("start streaming: %w", err)
	}

	// Transition to CDC.
	p.setPhase("streaming")
	p.logger.Info().Msg("COPY complete, switching to CDC streaming")

	for _, t := range tables {
		p.Metrics.TableStreaming(t.Schema, t.Name)
	}

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

// SlotInfo holds information about an existing replication slot.
type SlotInfo struct {
	SlotName      string
	ConfirmedLSN  pglogrepl.LSN
	RestartLSN    pglogrepl.LSN
	Active        bool
}

// checkSlot queries the source for the replication slot and returns its info.
func (p *Pipeline) checkSlot(ctx context.Context) (*SlotInfo, error) {
	var slotName string
	var confirmedFlush, restart *string
	var active bool

	err := p.srcPool.QueryRow(ctx, `
		SELECT slot_name, confirmed_flush_lsn::text, restart_lsn::text, active
		FROM pg_replication_slots
		WHERE slot_name = $1`, p.cfg.Replication.SlotName).Scan(&slotName, &confirmedFlush, &restart, &active)
	if err != nil {
		return nil, fmt.Errorf("slot %q not found: %w", p.cfg.Replication.SlotName, err)
	}

	info := &SlotInfo{SlotName: slotName, Active: active}
	if confirmedFlush != nil {
		lsn, err := pglogrepl.ParseLSN(*confirmedFlush)
		if err != nil {
			return nil, fmt.Errorf("parse confirmed_flush_lsn: %w", err)
		}
		info.ConfirmedLSN = lsn
	}
	if restart != nil {
		lsn, err := pglogrepl.ParseLSN(*restart)
		if err != nil {
			return nil, fmt.Errorf("parse restart_lsn: %w", err)
		}
		info.RestartLSN = lsn
	}
	return info, nil
}

// RunResumeCloneAndFollow resumes a previously interrupted clone:
// 1. Verifies the replication slot still exists (WAL is preserved)
// 2. Compares source vs dest row counts to find incomplete tables
// 3. Truncates and re-COPYs only incomplete tables (without snapshot)
// 4. Starts CDC streaming from the slot's LSN
func (p *Pipeline) RunResumeCloneAndFollow(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	// Ensure schema exists on destination (idempotent).
	p.setPhase("schema")
	p.logger.Info().Msg("dumping schema from source")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	p.logger.Info().Msg("applying schema to destination")
	if err := p.schemaMgr.ApplySchema(ctx, ddl); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	// Check that the replication slot survived.
	p.setPhase("resuming")
	slotInfo, err := p.checkSlot(ctx)
	if err != nil {
		return fmt.Errorf("cannot resume: %w — run a full clone instead", err)
	}
	if slotInfo.Active {
		return fmt.Errorf("cannot resume: slot %q is active (another process is using it)", slotInfo.SlotName)
	}

	startLSN := slotInfo.RestartLSN
	if slotInfo.ConfirmedLSN > startLSN {
		startLSN = slotInfo.ConfirmedLSN
	}
	p.logger.Info().
		Stringer("restart_lsn", slotInfo.RestartLSN).
		Stringer("confirmed_lsn", slotInfo.ConfirmedLSN).
		Stringer("start_lsn", startLSN).
		Msg("replication slot found, WAL is preserved")

	// List source tables and check dest completeness.
	srcTables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	var incompleteTables []snapshot.TableInfo
	var completeTables []snapshot.TableInfo
	for _, t := range srcTables {
		destCount, err := p.copier.DestRowCount(ctx, t.Schema, t.Name)
		if err != nil {
			return fmt.Errorf("check dest row count for %s: %w", t.QualifiedName(), err)
		}
		if destCount < t.RowCount {
			p.logger.Info().
				Str("table", t.QualifiedName()).
				Int64("source_rows", t.RowCount).
				Int64("dest_rows", destCount).
				Msg("incomplete table — will truncate and re-copy")
			incompleteTables = append(incompleteTables, t)
		} else {
			p.logger.Info().
				Str("table", t.QualifiedName()).
				Int64("rows", destCount).
				Msg("table complete — skipping")
			completeTables = append(completeTables, t)
		}
	}

	p.mu.Lock()
	p.progress.TablesTotal = len(srcTables)
	p.progress.TablesCopied = len(completeTables)
	p.mu.Unlock()

	p.initTableMetrics(srcTables)
	for _, t := range completeTables {
		p.Metrics.TableDone(t.Schema, t.Name, t.RowCount)
	}

	if len(incompleteTables) > 0 {
		p.setPhase("copy")
		p.logger.Info().Int("tables", len(incompleteTables)).Msg("truncating and re-copying incomplete tables")

		for _, t := range incompleteTables {
			p.logger.Info().Str("table", t.QualifiedName()).Msg("truncating")
			if err := p.copier.TruncateTable(ctx, t.Schema, t.Name); err != nil {
				return fmt.Errorf("truncate %s: %w", t.QualifiedName(), err)
			}
		}

		results := p.copier.CopyAll(ctx, incompleteTables, "")
		for _, r := range results {
			if r.Err != nil {
				p.Metrics.RecordError(r.Err)
				return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
			}
			p.Metrics.RecordApplied(0, 0, r.Table.SizeBytes)
		}
	} else {
		p.logger.Info().Msg("all tables complete — skipping COPY phase")
	}

	// Start streaming from the slot's LSN. The decoder won't create a new slot.
	p.decoder = stream.NewDecoder(p.replConn, p.cfg.Replication.SlotName, p.cfg.Replication.Publication, p.logger)
	p.decoder.CreateSlot(ctx, startLSN) //nolint:errcheck
	msgCh, err := p.decoder.StartStreaming(ctx)
	if err != nil {
		return fmt.Errorf("start streaming: %w", err)
	}

	p.setPhase("streaming")
	p.logger.Info().Msg("resumed CDC streaming")

	for _, t := range srcTables {
		p.Metrics.TableStreaming(t.Schema, t.Name)
	}

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
