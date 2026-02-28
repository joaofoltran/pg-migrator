package stream

import (
	"time"

	"github.com/jackc/pglogrepl"
)

// MessageKind identifies the type of message flowing through the pipeline.
type MessageKind int

const (
	KindBegin    MessageKind = iota
	KindCommit
	KindChange
	KindRelation
	KindSentinel
)

// String returns a human-readable name for a MessageKind.
func (k MessageKind) String() string {
	switch k {
	case KindBegin:
		return "Begin"
	case KindCommit:
		return "Commit"
	case KindChange:
		return "Change"
	case KindRelation:
		return "Relation"
	case KindSentinel:
		return "Sentinel"
	default:
		return "Unknown"
	}
}

// Message is the architectural spine of the pipeline.
// Both WAL changes and synthetic sentinels implement this interface.
type Message interface {
	Kind() MessageKind
	LSN() pglogrepl.LSN
	OriginID() string
	Timestamp() time.Time
}

// ChangeOp represents the DML operation type.
type ChangeOp int

const (
	OpInsert ChangeOp = iota
	OpUpdate
	OpDelete
)

// String returns a human-readable name for a ChangeOp.
func (o ChangeOp) String() string {
	switch o {
	case OpInsert:
		return "INSERT"
	case OpUpdate:
		return "UPDATE"
	case OpDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// Column describes a single column in a tuple.
type Column struct {
	Name     string
	DataType uint32
	Value    []byte
}

// TupleData holds the column values for a row.
type TupleData struct {
	Columns []Column
}

// BeginMessage marks the start of a transaction.
type BeginMessage struct {
	TxnLSN  pglogrepl.LSN
	TxnTime time.Time
	XID     uint32
}

func (m *BeginMessage) Kind() MessageKind     { return KindBegin }
func (m *BeginMessage) LSN() pglogrepl.LSN    { return m.TxnLSN }
func (m *BeginMessage) OriginID() string       { return "" }
func (m *BeginMessage) Timestamp() time.Time   { return m.TxnTime }

// CommitMessage marks the end of a transaction.
type CommitMessage struct {
	CommitLSN pglogrepl.LSN
	TxnTime   time.Time
}

func (m *CommitMessage) Kind() MessageKind     { return KindCommit }
func (m *CommitMessage) LSN() pglogrepl.LSN    { return m.CommitLSN }
func (m *CommitMessage) OriginID() string       { return "" }
func (m *CommitMessage) Timestamp() time.Time   { return m.TxnTime }

// RelationMessage carries schema metadata for a relation (table).
type RelationMessage struct {
	RelationID uint32
	Namespace  string
	Name       string
	Columns    []Column
	MsgLSN    pglogrepl.LSN
	MsgTime   time.Time
}

func (m *RelationMessage) Kind() MessageKind     { return KindRelation }
func (m *RelationMessage) LSN() pglogrepl.LSN    { return m.MsgLSN }
func (m *RelationMessage) OriginID() string       { return "" }
func (m *RelationMessage) Timestamp() time.Time   { return m.MsgTime }

// ChangeMessage represents an INSERT, UPDATE, or DELETE.
type ChangeMessage struct {
	Op         ChangeOp
	RelationID uint32
	Namespace  string
	Table      string
	OldTuple   *TupleData
	NewTuple   *TupleData
	MsgLSN    pglogrepl.LSN
	MsgTime   time.Time
	Origin    string
}

func (m *ChangeMessage) Kind() MessageKind     { return KindChange }
func (m *ChangeMessage) LSN() pglogrepl.LSN    { return m.MsgLSN }
func (m *ChangeMessage) OriginID() string       { return m.Origin }
func (m *ChangeMessage) Timestamp() time.Time   { return m.MsgTime }
