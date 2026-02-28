package bidi

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/migration/stream"
)

// Filter drops messages that originated from a specific replication origin,
// preventing infinite loops in bidirectional replication.
type Filter struct {
	originID string
	logger   zerolog.Logger
}

// NewFilter creates a Filter that drops messages matching the given origin ID.
func NewFilter(originID string, logger zerolog.Logger) *Filter {
	return &Filter{
		originID: originID,
		logger:   logger.With().Str("component", "bidi-filter").Logger(),
	}
}

// Run reads messages from the input channel, drops any whose OriginID matches
// the filter's origin, and forwards the rest to the returned output channel.
func (f *Filter) Run(ctx context.Context, in <-chan stream.Message) <-chan stream.Message {
	out := make(chan stream.Message, cap(in))

	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-in:
				if !ok {
					return
				}
				if msg.OriginID() == f.originID && f.originID != "" {
					f.logger.Debug().
						Str("origin", msg.OriginID()).
						Stringer("lsn", msg.LSN()).
						Msg("dropped looped message")
					continue
				}
				select {
				case out <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out
}

// Manager sets up bidirectional replication by wiring two decoder→filter→applier
// pipelines (one per direction).
type Manager struct {
	OriginA string
	OriginB string
	logger  zerolog.Logger
}

// NewManager creates a bidirectional replication Manager.
func NewManager(originA, originB string, logger zerolog.Logger) *Manager {
	return &Manager{
		OriginA: originA,
		OriginB: originB,
		logger:  logger.With().Str("component", "bidi-manager").Logger(),
	}
}

// Start sets up the bidirectional pipeline. In a full implementation this
// would wire two decoder→filter→applier chains. Currently a placeholder
// that logs the configuration.
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info().
		Str("origin_a", m.OriginA).
		Str("origin_b", m.OriginB).
		Msg("bidirectional replication manager started")
	<-ctx.Done()
	return ctx.Err()
}
