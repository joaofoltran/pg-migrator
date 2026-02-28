package stream

import (
	"context"
	"fmt"
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

	mu            sync.Mutex
	confirmedLSN  pglogrepl.LSN
	lastStatusTime time.Time

	cancel context.CancelFunc
	done   chan struct{}
}

// NewDecoder creates a Decoder that will stream from the given replication connection.
func NewDecoder(conn *pgconn.PgConn, slotName, publication string, logger zerolog.Logger) *Decoder {
	return &Decoder{
		conn:        conn,
		logger:      logger.With().Str("component", "decoder").Logger(),
		slotName:    slotName,
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

	result, err := pglogrepl.CreateReplicationSlot(ctx, d.conn, d.slotName, "pgoutput",
		pglogrepl.CreateReplicationSlotOptions{Temporary: false})
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

	ch := make(chan Message, 256)
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

	standbyInterval := 10 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Send periodic standby status.
		if time.Since(d.lastStatusTime) >= standbyInterval {
			d.mu.Lock()
			lsn := d.confirmedLSN
			d.mu.Unlock()
			if err := d.sendStandbyStatus(ctx, lsn); err != nil {
				d.logger.Err(err).Msg("failed to send standby status")
			}
		}

		recvCtx, cancel := context.WithDeadline(ctx, time.Now().Add(standbyInterval))
		rawMsg, err := d.conn.ReceiveMessage(recvCtx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Timeout is expected — loop around to send standby status.
			if pgconn.Timeout(err) {
				continue
			}
			d.logger.Err(err).Msg("receive message failed")
			return
		}

		if errResp, ok := rawMsg.(*pgproto3.ErrorResponse); ok {
			d.logger.Error().Str("severity", errResp.Severity).Str("message", errResp.Message).Msg("server error")
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
			if pkm.ReplyRequested {
				d.mu.Lock()
				lsn := d.confirmedLSN
				d.mu.Unlock()
				if err := d.sendStandbyStatus(ctx, lsn); err != nil {
					d.logger.Err(err).Msg("keepalive reply failed")
				}
			}

		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(copyData.Data[1:])
			if err != nil {
				d.logger.Err(err).Msg("parse xlogdata")
				continue
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
		d.emit(ctx, ch, &BeginMessage{
			TxnLSN:  pglogrepl.LSN(msg.FinalLSN),
			TxnTime: msg.CommitTime,
			XID:     msg.Xid,
		})

	case *pglogrepl.CommitMessage:
		d.emit(ctx, ch, &CommitMessage{
			CommitLSN: pglogrepl.LSN(msg.CommitLSN),
			TxnTime:   msg.CommitTime,
		})

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
		d.emit(ctx, ch, rel)

	case *pglogrepl.InsertMessage:
		rel := d.relations[msg.RelationID]
		if rel == nil {
			d.logger.Warn().Uint32("relation_id", msg.RelationID).Msg("unknown relation for insert")
			return
		}
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
	select {
	case ch <- msg:
	case <-ctx.Done():
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
