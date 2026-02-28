package stream

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/rs/zerolog"
)

// Decoder consumes WAL data via pglogrepl and emits Messages on a channel.
type Decoder struct {
	conn   *pgconn.PgConn
	logger zerolog.Logger

	slotName    string
	publication string
	startLSN    pglogrepl.LSN

	relations map[uint32]*RelationMessage
	origin    string // current origin from OriginMessage

	pendingBegin   *BeginMessage
	emptyTxSkipped int64

	mu             sync.Mutex
	confirmedLSN   pglogrepl.LSN
	serverWALEnd   pglogrepl.LSN
	lastStatusTime time.Time
	loopErr        error

	cancel context.CancelFunc
	done   chan struct{}
}

// NewDecoder creates a Decoder that will stream from the given replication connection.
func NewDecoder(conn *pgconn.PgConn, slotName, publication string, logger zerolog.Logger) *Decoder {
	return &Decoder{
		conn:        conn,
		logger:      logger.With().Str("component", "decoder").Logger(),
		slotName:    strings.ReplaceAll(slotName, "-", "_"),
		publication: publication,
		relations:   make(map[uint32]*RelationMessage),
		done:        make(chan struct{}),
	}
}

// CreateSlot creates a replication slot and returns the exported snapshot name.
// The snapshot remains valid until StartStreaming is called, so callers must
// complete their COPY phase using the snapshot before calling StartStreaming.
// If startLSN is non-zero, no slot is created and the snapshot name is empty.
func (d *Decoder) CreateSlot(ctx context.Context, startLSN pglogrepl.LSN) (string, error) {
	d.startLSN = startLSN

	if startLSN != 0 {
		return "", nil
	}

	sql := fmt.Sprintf(`CREATE_REPLICATION_SLOT %s LOGICAL pgoutput (SNAPSHOT 'export')`, d.slotName)
	result, err := pglogrepl.ParseCreateReplicationSlot(d.conn.Exec(ctx, sql))
	if err != nil {
		return "", fmt.Errorf("create replication slot: %w", err)
	}
	parsedLSN, err := pglogrepl.ParseLSN(result.ConsistentPoint)
	if err != nil {
		return "", fmt.Errorf("parse consistent point LSN: %w", err)
	}
	d.startLSN = parsedLSN
	d.logger.Info().
		Str("slot", d.slotName).
		Str("snapshot", result.SnapshotName).
		Stringer("lsn", d.startLSN).
		Msg("created replication slot")

	return result.SnapshotName, nil
}

// StartLSN returns the LSN that will be used when streaming begins.
func (d *Decoder) StartLSN() pglogrepl.LSN {
	return d.startLSN
}

// StartStreaming begins consuming WAL from the replication slot. This
// invalidates the snapshot returned by CreateSlot, so it must only be
// called after the COPY phase is complete.
func (d *Decoder) StartStreaming(ctx context.Context) (<-chan Message, error) {
	err := pglogrepl.StartReplication(ctx, d.conn, d.slotName, d.startLSN,
		pglogrepl.StartReplicationOptions{
			PluginArgs: []string{
				"proto_version '1'",
				fmt.Sprintf("publication_names '%s'", d.publication),
			},
		})
	if err != nil {
		return nil, fmt.Errorf("start replication: %w", err)
	}

	d.confirmedLSN = d.startLSN
	d.lastStatusTime = time.Now()

	ch := make(chan Message, 4096)
	ctx, d.cancel = context.WithCancel(ctx)
	go d.receiveLoop(ctx, ch)

	return ch, nil
}

// Start is a convenience that calls CreateSlot followed by StartStreaming.
// WARNING: The snapshot returned is already invalid because StartStreaming
// has been called. Use CreateSlot + StartStreaming separately when you need
// to perform COPY using the snapshot.
func (d *Decoder) Start(ctx context.Context, startLSN pglogrepl.LSN) (<-chan Message, string, error) {
	snapshotName, err := d.CreateSlot(ctx, startLSN)
	if err != nil {
		return nil, "", err
	}
	ch, err := d.StartStreaming(ctx)
	if err != nil {
		return nil, "", err
	}
	return ch, snapshotName, nil
}

func (d *Decoder) receiveLoop(ctx context.Context, ch chan<- Message) {
	defer close(ch)
	defer close(d.done)

	standbyInterval := 1 * time.Second
	recvTimeout := 2 * time.Second
	var msgCount int64
	lastDiag := time.Now()

	setErr := func(err error) {
		d.mu.Lock()
		d.loopErr = err
		d.mu.Unlock()
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if time.Since(d.lastStatusTime) >= standbyInterval {
			if err := d.sendStandbyStatus(ctx, d.effectiveLSN(ch)); err != nil {
				d.logger.Err(err).Msg("failed to send standby status")
			}
		}

		recvCtx, cancel := context.WithDeadline(ctx, time.Now().Add(recvTimeout))
		rawMsg, err := d.conn.ReceiveMessage(recvCtx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if pgconn.Timeout(err) {
				continue
			}
			d.logger.Err(err).Msg("receive message failed")
			setErr(fmt.Errorf("receive message: %w", err))
			return
		}

		if errResp, ok := rawMsg.(*pgproto3.ErrorResponse); ok {
			d.logger.Error().
				Str("severity", errResp.Severity).
				Str("code", errResp.Code).
				Str("message", errResp.Message).
				Str("detail", errResp.Detail).
				Str("hint", errResp.Hint).
				Str("schema", errResp.SchemaName).
				Str("table", errResp.TableName).
				Str("column", errResp.ColumnName).
				Str("where", errResp.Where).
				Msg("server error from replication stream")
			setErr(fmt.Errorf("server error: %s: %s (SQLSTATE %s)", errResp.Severity, errResp.Message, errResp.Code))
			return
		}

		copyData, ok := rawMsg.(*pgproto3.CopyData)
		if !ok {
			continue
		}

		switch copyData.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(copyData.Data[1:])
			if err != nil {
				d.logger.Err(err).Msg("parse keepalive")
				continue
			}
				d.mu.Lock()
			if pglogrepl.LSN(pkm.ServerWALEnd) > d.serverWALEnd {
				d.serverWALEnd = pglogrepl.LSN(pkm.ServerWALEnd)
			}
			d.mu.Unlock()

			if pkm.ReplyRequested {
				if err := d.sendStandbyStatus(ctx, d.effectiveLSN(ch)); err != nil {
					d.logger.Err(err).Msg("keepalive reply failed")
				}
			}

		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(copyData.Data[1:])
			if err != nil {
				d.logger.Err(err).Msg("parse xlogdata")
				continue
			}

			d.mu.Lock()
			if pglogrepl.LSN(xld.ServerWALEnd) > d.serverWALEnd {
				d.serverWALEnd = pglogrepl.LSN(xld.ServerWALEnd)
			}
			d.mu.Unlock()

			msgCount++
			if time.Since(lastDiag) >= 10*time.Second {
				d.mu.Lock()
				lsn := d.confirmedLSN
				d.mu.Unlock()
				d.logger.Info().
					Int64("msgs", msgCount).
					Int("ch_len", len(ch)).
					Int("ch_cap", cap(ch)).
					Stringer("wal_pos", pglogrepl.LSN(xld.WALStart)).
					Stringer("confirmed", lsn).
					Int64("empty_tx_skipped", d.emptyTxSkipped).
					Msg("decoder throughput")
				lastDiag = time.Now()
			}
			d.decodeWALData(ctx, ch, xld)
		}
	}
}

func (d *Decoder) decodeWALData(ctx context.Context, ch chan<- Message, xld pglogrepl.XLogData) {
	logicalMsg, err := pglogrepl.Parse(xld.WALData)
	if err != nil {
		d.logger.Err(err).Msg("parse WAL data")
		return
	}

	walLSN := pglogrepl.LSN(xld.WALStart)
	now := time.Now()

	switch msg := logicalMsg.(type) {
	case *pglogrepl.BeginMessage:
		d.pendingBegin = &BeginMessage{
			TxnLSN:  pglogrepl.LSN(msg.FinalLSN),
			TxnTime: msg.CommitTime,
			XID:     msg.Xid,
		}

	case *pglogrepl.CommitMessage:
		if d.pendingBegin != nil {
			d.emptyTxSkipped++
			d.pendingBegin = nil
		} else {
			d.emit(ctx, ch, &CommitMessage{
				CommitLSN: pglogrepl.LSN(msg.CommitLSN),
				TxnTime:   msg.CommitTime,
			})
		}

	case *pglogrepl.RelationMessage:
		cols := make([]Column, len(msg.Columns))
		for i, c := range msg.Columns {
			cols[i] = Column{Name: c.Name, DataType: c.DataType}
		}
		rel := &RelationMessage{
			RelationID: msg.RelationID,
			Namespace:  msg.Namespace,
			Name:       msg.RelationName,
			Columns:    cols,
			MsgLSN:    walLSN,
			MsgTime:   now,
		}
		d.relations[msg.RelationID] = rel
		d.flushPendingBegin(ctx, ch)
		d.emit(ctx, ch, rel)

	case *pglogrepl.InsertMessage:
		rel := d.relations[msg.RelationID]
		if rel == nil {
			d.logger.Warn().Uint32("relation_id", msg.RelationID).Msg("unknown relation for insert")
			return
		}
		d.flushPendingBegin(ctx, ch)
		d.emit(ctx, ch, &ChangeMessage{
			Op:         OpInsert,
			RelationID: msg.RelationID,
			Namespace:  rel.Namespace,
			Table:      rel.Name,
			NewTuple:   decodeTuple(msg.Tuple, rel.Columns),
			MsgLSN:    walLSN,
			MsgTime:   now,
			Origin:    d.origin,
		})

	case *pglogrepl.UpdateMessage:
		rel := d.relations[msg.RelationID]
		if rel == nil {
			d.logger.Warn().Uint32("relation_id", msg.RelationID).Msg("unknown relation for update")
			return
		}
		d.flushPendingBegin(ctx, ch)
		cm := &ChangeMessage{
			Op:         OpUpdate,
			RelationID: msg.RelationID,
			Namespace:  rel.Namespace,
			Table:      rel.Name,
			NewTuple:   decodeTuple(msg.NewTuple, rel.Columns),
			MsgLSN:    walLSN,
			MsgTime:   now,
			Origin:    d.origin,
		}
		if msg.OldTuple != nil {
			cm.OldTuple = decodeTuple(msg.OldTuple, rel.Columns)
		}
		d.emit(ctx, ch, cm)

	case *pglogrepl.DeleteMessage:
		rel := d.relations[msg.RelationID]
		if rel == nil {
			d.logger.Warn().Uint32("relation_id", msg.RelationID).Msg("unknown relation for delete")
			return
		}
		d.flushPendingBegin(ctx, ch)
		d.emit(ctx, ch, &ChangeMessage{
			Op:         OpDelete,
			RelationID: msg.RelationID,
			Namespace:  rel.Namespace,
			Table:      rel.Name,
			OldTuple:   decodeTuple(msg.OldTuple, rel.Columns),
			MsgLSN:    walLSN,
			MsgTime:   now,
			Origin:    d.origin,
		})

	case *pglogrepl.OriginMessage:
		d.origin = msg.Name
	}
}

func (d *Decoder) flushPendingBegin(ctx context.Context, ch chan<- Message) {
	if d.pendingBegin != nil {
		d.emit(ctx, ch, d.pendingBegin)
		d.pendingBegin = nil
	}
}

func decodeTuple(tuple *pglogrepl.TupleData, cols []Column) *TupleData {
	if tuple == nil {
		return nil
	}
	td := &TupleData{Columns: make([]Column, len(tuple.Columns))}
	for i, c := range tuple.Columns {
		col := Column{Value: c.Data}
		if i < len(cols) {
			col.Name = cols[i].Name
			col.DataType = cols[i].DataType
		}
		td.Columns[i] = col
	}
	return td
}

func (d *Decoder) emit(ctx context.Context, ch chan<- Message, msg Message) {
	for {
		select {
		case ch <- msg:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Channel is full. Send a standby heartbeat while waiting so the
		// source doesn't time us out due to backpressure stalls.
		if time.Since(d.lastStatusTime) >= 1*time.Second {
			d.mu.Lock()
			lsn := d.confirmedLSN
			d.mu.Unlock()
			if err := d.sendStandbyStatus(ctx, lsn); err != nil {
				d.logger.Err(err).Msg("emit backpressure: standby status failed")
			}
		}

		// Brief wait before retrying so we don't spin-lock.
		t := time.NewTimer(100 * time.Millisecond)
		select {
		case ch <- msg:
			t.Stop()
			return
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return
		}
	}
}

func (d *Decoder) sendStandbyStatus(ctx context.Context, lsn pglogrepl.LSN) error {
	d.lastStatusTime = time.Now()
	return pglogrepl.SendStandbyStatusUpdate(ctx, d.conn,
		pglogrepl.StandbyStatusUpdate{
			WALWritePosition: lsn,
			WALFlushPosition: lsn,
			WALApplyPosition: lsn,
		})
}

// effectiveLSN returns the best LSN to report to the server. If the applier
// channel is drained (we're caught up) and the server's WAL end is ahead of
// the last confirmed applier LSN, report the server's position so the slot
// doesn't fall behind during idle periods.
func (d *Decoder) effectiveLSN(ch chan<- Message) pglogrepl.LSN {
	d.mu.Lock()
	confirmed := d.confirmedLSN
	serverEnd := d.serverWALEnd
	d.mu.Unlock()

	if len(ch) == 0 && serverEnd > confirmed {
		return serverEnd
	}
	return confirmed
}

// Err returns the error that caused the receive loop to exit, if any.
// It is safe to call after the message channel has been closed.
func (d *Decoder) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loopErr
}

// ConfirmLSN advances the confirmed flush position for the replication slot.
func (d *Decoder) ConfirmLSN(lsn pglogrepl.LSN) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if lsn > d.confirmedLSN {
		d.confirmedLSN = lsn
	}
}

// Close shuts down the decoder and waits for the receive loop to exit.
func (d *Decoder) Close() {
	if d.cancel != nil {
		d.cancel()
		<-d.done
	}
}
