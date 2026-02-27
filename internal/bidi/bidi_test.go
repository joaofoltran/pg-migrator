package bidi

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmigrator/internal/stream"
)

func TestFilter_DropsMatchingOrigin(t *testing.T) {
	logger := zerolog.Nop()
	f := NewFilter("pgmigrator-a", logger)

	in := make(chan stream.Message, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := f.Run(ctx, in)

	// Send a message with matching origin (should be dropped).
	in <- &stream.ChangeMessage{
		Op:      stream.OpInsert,
		MsgLSN:  pglogrepl.LSN(100),
		MsgTime: time.Now(),
		Origin:  "pgmigrator-a",
	}

	// Send a message with different origin (should pass through).
	msg := &stream.ChangeMessage{
		Op:      stream.OpInsert,
		MsgLSN:  pglogrepl.LSN(200),
		MsgTime: time.Now(),
		Origin:  "pgmigrator-b",
	}
	in <- msg

	// Send a message with no origin (should pass through).
	noOriginMsg := &stream.BeginMessage{TxnLSN: pglogrepl.LSN(300), TxnTime: time.Now()}
	in <- noOriginMsg

	close(in)

	var received []stream.Message
	for m := range out {
		received = append(received, m)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(received))
	}

	if received[0].LSN() != pglogrepl.LSN(200) {
		t.Errorf("first passed message LSN = %v, want 200", received[0].LSN())
	}
	if received[1].LSN() != pglogrepl.LSN(300) {
		t.Errorf("second passed message LSN = %v, want 300", received[1].LSN())
	}
}

func TestFilter_EmptyOriginPassesAll(t *testing.T) {
	logger := zerolog.Nop()
	f := NewFilter("", logger)

	in := make(chan stream.Message, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := f.Run(ctx, in)

	in <- &stream.ChangeMessage{Origin: "any-origin", MsgLSN: pglogrepl.LSN(100), MsgTime: time.Now()}
	in <- &stream.BeginMessage{TxnLSN: pglogrepl.LSN(200), TxnTime: time.Now()}
	close(in)

	var count int
	for range out {
		count++
	}
	if count != 2 {
		t.Errorf("expected all 2 messages to pass through, got %d", count)
	}
}

func TestFilter_ContextCancellation(t *testing.T) {
	logger := zerolog.Nop()
	f := NewFilter("origin", logger)

	in := make(chan stream.Message, 10)
	ctx, cancel := context.WithCancel(context.Background())

	out := f.Run(ctx, in)
	cancel()

	// Output channel should close eventually.
	select {
	case _, ok := <-out:
		if ok {
			t.Error("expected channel to close after context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Error("output channel did not close after context cancellation")
	}
}
