package stream

import (
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
)

func TestMessageKindString(t *testing.T) {
	tests := []struct {
		kind MessageKind
		want string
	}{
		{KindBegin, "Begin"},
		{KindCommit, "Commit"},
		{KindChange, "Change"},
		{KindRelation, "Relation"},
		{KindSentinel, "Sentinel"},
		{MessageKind(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("MessageKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestChangeOpString(t *testing.T) {
	tests := []struct {
		op   ChangeOp
		want string
	}{
		{OpInsert, "INSERT"},
		{OpUpdate, "UPDATE"},
		{OpDelete, "DELETE"},
		{ChangeOp(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("ChangeOp(%d).String() = %q, want %q", tt.op, got, tt.want)
		}
	}
}

func TestBeginMessage(t *testing.T) {
	now := time.Now()
	m := &BeginMessage{TxnLSN: pglogrepl.LSN(100), TxnTime: now, XID: 42}

	if m.Kind() != KindBegin {
		t.Errorf("Kind() = %v, want KindBegin", m.Kind())
	}
	if m.LSN() != pglogrepl.LSN(100) {
		t.Errorf("LSN() = %v, want 100", m.LSN())
	}
	if m.OriginID() != "" {
		t.Errorf("OriginID() = %q, want empty", m.OriginID())
	}
	if !m.Timestamp().Equal(now) {
		t.Errorf("Timestamp() = %v, want %v", m.Timestamp(), now)
	}
}

func TestCommitMessage(t *testing.T) {
	now := time.Now()
	m := &CommitMessage{CommitLSN: pglogrepl.LSN(200), TxnTime: now}

	if m.Kind() != KindCommit {
		t.Errorf("Kind() = %v, want KindCommit", m.Kind())
	}
	if m.LSN() != pglogrepl.LSN(200) {
		t.Errorf("LSN() = %v, want 200", m.LSN())
	}
}

func TestRelationMessage(t *testing.T) {
	m := &RelationMessage{
		RelationID: 1,
		Namespace:  "public",
		Name:       "users",
		Columns:    []Column{{Name: "id", DataType: 23}},
		MsgLSN:     pglogrepl.LSN(300),
		MsgTime:    time.Now(),
	}

	if m.Kind() != KindRelation {
		t.Errorf("Kind() = %v, want KindRelation", m.Kind())
	}
	if m.OriginID() != "" {
		t.Errorf("OriginID() = %q, want empty", m.OriginID())
	}
}

func TestChangeMessage(t *testing.T) {
	m := &ChangeMessage{
		Op:         OpInsert,
		RelationID: 1,
		Namespace:  "public",
		Table:      "users",
		NewTuple:   &TupleData{Columns: []Column{{Name: "id", Value: []byte("1")}}},
		MsgLSN:     pglogrepl.LSN(400),
		MsgTime:    time.Now(),
		Origin:     "origin-a",
	}

	if m.Kind() != KindChange {
		t.Errorf("Kind() = %v, want KindChange", m.Kind())
	}
	if m.OriginID() != "origin-a" {
		t.Errorf("OriginID() = %q, want origin-a", m.OriginID())
	}
}

func TestChangeMessageNoOrigin(t *testing.T) {
	m := &ChangeMessage{Op: OpUpdate}
	if m.OriginID() != "" {
		t.Errorf("OriginID() = %q, want empty for no origin", m.OriginID())
	}
}
