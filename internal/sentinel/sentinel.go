package sentinel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmigrator/internal/stream"
)

// SentinelMessage is a synthetic message injected into the pipeline for
// zero-downtime switchover coordination ("magic chicken").
type SentinelMessage struct {
	ID      string
	SentLSN pglogrepl.LSN
	SentAt  time.Time
}

func (m *SentinelMessage) Kind() stream.MessageKind  { return stream.KindSentinel }
func (m *SentinelMessage) LSN() pglogrepl.LSN        { return m.SentLSN }
func (m *SentinelMessage) OriginID() string           { return "" }
func (m *SentinelMessage) Timestamp() time.Time       { return m.SentAt }

// confirmation holds the result of a sentinel round-trip.
type confirmation struct {
	confirmedAt time.Time
}

// Coordinator manages sentinel injection and confirmation.
type Coordinator struct {
	logger zerolog.Logger
	out    chan<- stream.Message

	mu           sync.Mutex
	pending      map[string]chan confirmation
	nextID       int
}

// NewCoordinator creates a Coordinator that injects sentinels into the given channel.
func NewCoordinator(out chan<- stream.Message, logger zerolog.Logger) *Coordinator {
	return &Coordinator{
		logger:  logger.With().Str("component", "sentinel").Logger(),
		out:     out,
		pending: make(map[string]chan confirmation),
	}
}

// Initiate injects a new sentinel message at the given LSN position.
func (c *Coordinator) Initiate(ctx context.Context, lsn pglogrepl.LSN) (string, error) {
	c.mu.Lock()
	c.nextID++
	id := fmt.Sprintf("sentinel-%d", c.nextID)
	ch := make(chan confirmation, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := &SentinelMessage{
		ID:      id,
		SentLSN: lsn,
		SentAt:  time.Now(),
	}

	select {
	case c.out <- msg:
		c.logger.Info().Str("id", id).Stringer("lsn", lsn).Msg("sentinel injected")
		return id, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return "", ctx.Err()
	}
}

// WaitForConfirmation blocks until the sentinel with the given ID is confirmed
// by the applier, or the timeout elapses.
func (c *Coordinator) WaitForConfirmation(id string, timeout time.Duration) error {
	c.mu.Lock()
	ch, ok := c.pending[id]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown sentinel: %s", id)
	}

	select {
	case conf := <-ch:
		c.logger.Info().
			Str("id", id).
			Dur("roundtrip", time.Since(conf.confirmedAt)).
			Msg("sentinel confirmed")
		return nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("sentinel %s timed out after %s", id, timeout)
	}
}

// Confirm is called by the applier when it encounters a SentinelMessage.
func (c *Coordinator) Confirm(id string) {
	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if ok {
		ch <- confirmation{confirmedAt: time.Now()}
	}
}
