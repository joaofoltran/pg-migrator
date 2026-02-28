package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/config"
	"github.com/jfoltran/pgmanager/internal/metrics"
	"github.com/jfoltran/pgmanager/internal/migration/pipeline"
)

// JobManager manages the currently running pipeline job.
// Only one job can run at a time.
type JobManager struct {
	logger    zerolog.Logger
	collector *metrics.Collector

	mu       sync.Mutex
	pipeline *pipeline.Pipeline
	cancel   context.CancelFunc
	jobErr   error
	running  bool
}

// NewJobManager creates a new JobManager.
func NewJobManager(collector *metrics.Collector, logger zerolog.Logger) *JobManager {
	return &JobManager{
		logger:    logger.With().Str("component", "job-manager").Logger(),
		collector: collector,
	}
}

// IsRunning returns true if a job is currently active.
func (jm *JobManager) IsRunning() bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.running
}

// LastError returns the error from the last completed job (nil if success or still running).
func (jm *JobManager) LastError() error {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.jobErr
}

// RunClone starts a clone job in the background.
func (jm *JobManager) RunClone(parentCtx context.Context, cfg *config.Config, follow, resume bool) error {
	jm.mu.Lock()
	if jm.running {
		jm.mu.Unlock()
		return fmt.Errorf("a job is already running")
	}
	jm.running = true
	jm.jobErr = nil
	jm.mu.Unlock()

	logWriter := metrics.NewLogWriter(jm.collector)
	pipelineLogger := zerolog.New(zerolog.MultiLevelWriter(jm.logger, logWriter)).
		With().Timestamp().Logger().Level(jm.logger.GetLevel())

	p := pipeline.New(cfg, pipelineLogger)
	p.SetLogger(pipelineLogger)

	ctx, cancel := context.WithCancel(parentCtx)

	jm.mu.Lock()
	jm.pipeline = p
	jm.cancel = cancel
	jm.mu.Unlock()

	go func() {
		var err error
		switch {
		case resume:
			err = p.RunResumeCloneAndFollow(ctx)
		case follow:
			err = p.RunCloneAndFollow(ctx)
		default:
			err = p.RunClone(ctx)
		}

		jm.mu.Lock()
		jm.running = false
		jm.jobErr = err
		jm.pipeline = nil
		jm.cancel = nil
		jm.mu.Unlock()

		p.Close()

		if err != nil && err != context.Canceled {
			jm.logger.Err(err).Msg("job finished with error")
		} else {
			jm.logger.Info().Msg("job finished successfully")
		}
	}()

	return nil
}

// RunFollow starts a follow (CDC-only) job in the background.
func (jm *JobManager) RunFollow(parentCtx context.Context, cfg *config.Config, startLSN string) error {
	jm.mu.Lock()
	if jm.running {
		jm.mu.Unlock()
		return fmt.Errorf("a job is already running")
	}
	jm.running = true
	jm.jobErr = nil
	jm.mu.Unlock()

	logWriter := metrics.NewLogWriter(jm.collector)
	pipelineLogger := zerolog.New(zerolog.MultiLevelWriter(jm.logger, logWriter)).
		With().Timestamp().Logger().Level(jm.logger.GetLevel())

	p := pipeline.New(cfg, pipelineLogger)
	p.SetLogger(pipelineLogger)

	ctx, cancel := context.WithCancel(parentCtx)

	jm.mu.Lock()
	jm.pipeline = p
	jm.cancel = cancel
	jm.mu.Unlock()

	go func() {
		var lsn pglogrepl.LSN
		if startLSN != "" {
			var parseErr error
			lsn, parseErr = pglogrepl.ParseLSN(startLSN)
			if parseErr != nil {
				jm.mu.Lock()
				jm.running = false
				jm.jobErr = parseErr
				jm.mu.Unlock()
				p.Close()
				return
			}
		}

		err := p.RunFollow(ctx, lsn)

		jm.mu.Lock()
		jm.running = false
		jm.jobErr = err
		jm.pipeline = nil
		jm.cancel = nil
		jm.mu.Unlock()

		p.Close()

		if err != nil && err != context.Canceled {
			jm.logger.Err(err).Msg("job finished with error")
		} else {
			jm.logger.Info().Msg("job finished successfully")
		}
	}()

	return nil
}

// StopJob cancels the currently running job.
func (jm *JobManager) StopJob() error {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if !jm.running || jm.cancel == nil {
		return fmt.Errorf("no job is running")
	}
	jm.cancel()
	return nil
}

// Pipeline returns the currently running pipeline (may be nil).
func (jm *JobManager) Pipeline() *pipeline.Pipeline {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.pipeline
}

// RunSwitchover starts a switchover job in the background.
func (jm *JobManager) RunSwitchover(parentCtx context.Context, cfg *config.Config, timeout time.Duration) error {
	jm.mu.Lock()
	if jm.running {
		jm.mu.Unlock()
		return fmt.Errorf("a job is already running")
	}
	jm.running = true
	jm.jobErr = nil
	jm.mu.Unlock()

	logWriter := metrics.NewLogWriter(jm.collector)
	pipelineLogger := zerolog.New(zerolog.MultiLevelWriter(jm.logger, logWriter)).
		With().Timestamp().Logger().Level(jm.logger.GetLevel())

	p := pipeline.New(cfg, pipelineLogger)
	p.SetLogger(pipelineLogger)

	ctx, cancel := context.WithCancel(parentCtx)

	jm.mu.Lock()
	jm.pipeline = p
	jm.cancel = cancel
	jm.mu.Unlock()

	go func() {
		err := p.RunSwitchover(ctx, timeout)

		jm.mu.Lock()
		jm.running = false
		jm.jobErr = err
		jm.pipeline = nil
		jm.cancel = nil
		jm.mu.Unlock()

		p.Close()

		if err != nil && err != context.Canceled {
			jm.logger.Err(err).Msg("switchover finished with error")
		} else {
			jm.logger.Info().Msg("switchover finished successfully")
		}
	}()

	return nil
}

// Collector returns the shared metrics collector.
func (jm *JobManager) Collector() *metrics.Collector {
	return jm.collector
}
