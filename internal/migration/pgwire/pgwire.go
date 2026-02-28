package pgwire

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
)

// Conn wraps a pgconn.PgConn with replication-specific helpers.
type Conn struct {
	conn   *pgconn.PgConn
	logger zerolog.Logger
}

// NewConn creates a Conn wrapper.
func NewConn(conn *pgconn.PgConn, logger zerolog.Logger) *Conn {
	return &Conn{
		conn:   conn,
		logger: logger.With().Str("component", "pgwire").Logger(),
	}
}

// Raw returns the underlying pgconn.PgConn.
func (c *Conn) Raw() *pgconn.PgConn {
	return c.conn
}

// SetReplicationOrigin configures a replication origin on the connection so
// that writes are tagged with the given origin name. This is used for
// bidirectional loop detection.
func (c *Conn) SetReplicationOrigin(ctx context.Context, originName string) error {
	// Create the origin if it doesn't exist.
	_, err := c.exec(ctx, fmt.Sprintf(
		"SELECT pg_replication_origin_create('%s') WHERE NOT EXISTS (SELECT 1 FROM pg_replication_origin WHERE roname = '%s')",
		originName, originName))
	if err != nil {
		return fmt.Errorf("create replication origin: %w", err)
	}

	// Set the session to use this origin.
	_, err = c.exec(ctx, fmt.Sprintf("SELECT pg_replication_origin_session_setup('%s')", originName))
	if err != nil {
		return fmt.Errorf("setup replication origin session: %w", err)
	}

	c.logger.Info().Str("origin", originName).Msg("replication origin configured")
	return nil
}

// DropReplicationSlot drops a replication slot if it exists.
func (c *Conn) DropReplicationSlot(ctx context.Context, slotName string) error {
	_, err := c.exec(ctx, fmt.Sprintf("SELECT pg_drop_replication_slot('%s')", slotName))
	if err != nil {
		return fmt.Errorf("drop replication slot: %w", err)
	}
	return nil
}

func (c *Conn) exec(ctx context.Context, sql string) ([]byte, error) {
	mrr := c.conn.Exec(ctx, sql)
	var result []byte
	for mrr.NextResult() {
		buf := mrr.ResultReader().Read()
		if buf.Err != nil {
			return nil, buf.Err
		}
	}
	return result, mrr.Close()
}

// Close closes the underlying connection.
func (c *Conn) Close(ctx context.Context) error {
	return c.conn.Close(ctx)
}
