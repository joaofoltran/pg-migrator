package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmigrator/internal/stream"
)

func TestSentinelMessage_Interface(t *testing.T) {
	now := time.Now()
	m := &SentinelMessage{
		ID:      "sentinel-1",
		SentLSN: pglogrepl.LSN(500),
		SentAt:  now,
	}

	if m.Kind() != stream.KindSentinel {
		t.Errorf("Kind() = %v, want KindSentinel", m.Kind())
	}
	if m.LSN() != pglogrepl.LSN(500) {
		t.Errorf("LSN() = %v, want 500", m.LSN())
	}
	if m.OriginID() != "" {
		t.Errorf("OriginID() = %q, want empty", m.OriginID())
	}
	if !m.Timestamp().Equal(now) {
		t.Errorf("Timestamp() = %v, want %v", m.Timestamp(), now)
	}
}

func TestCoordinator_InitiateAndConfirm(t *testing.T) {
	ch := make(chan stream.Message, 10)
	logger := zerolog.Nop()
	coord := NewCoordinator(ch, logger)

	ctx := context.Background()
	id, err := coord.Initiate(ctx, pglogrepl.LSN(100))
	if err != nil {
		t.Fatalf("Initiate() error: %v", err)
	}
	if id != "sentinel-1" {
		t.Errorf("Initiate() id = %q, want sentinel-1", id)
	}

	// Verify message was sent to channel.
	select {
	case msg := <-ch:
		sm, ok := msg.(*SentinelMessage)
		if !ok {
			t.Fatal("expected SentinelMessage")
		}
		if sm.ID != "sentinel-1" {
			t.Errorf("sent message ID = %q, want sentinel-1", sm.ID)
		}
	default:
		t.Fatal("no message on channel")
	}

	// Confirm should unblock WaitForConfirmation.
	done := make(chan error, 1)
	go func() {
		done <- coord.WaitForConfirmation(id, 5*time.Second)
	}()

	time.Sleep(10 * time.Millisecond)
	coord.Confirm(id)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("WaitForConfirmation() error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForConfirmation() timed out")
	}
}

func TestCoordinator_Timeout(t *testing.T) {
	ch := make(chan stream.Message, 10)
	logger := zerolog.Nop()
	coord := NewCoordinator(ch, logger)

	ctx := context.Background()
	id, err := coord.Initiate(ctx, pglogrepl.LSN(100))
	if err != nil {
		t.Fatalf("Initiate() error: %v", err)
	}

	err = coord.WaitForConfirmation(id, 50*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestCoordinator_UnknownSentinel(t *testing.T) {
	ch := make(chan stream.Message, 10)
	logger := zerolog.Nop()
	coord := NewCoordinator(ch, logger)

	err := coord.WaitForConfirmation("nonexistent", time.Second)
	if err == nil {
		t.Error("expected error for unknown sentinel")
	}
}

func TestCoordinator_ContextCancelled(t *testing.T) {
	ch := make(chan stream.Message) // unbuffered — will block
	logger := zerolog.Nop()
	coord := NewCoordinator(ch, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := coord.Initiate(ctx, pglogrepl.LSN(100))
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func TestCoordinator_MultipleIDs(t *testing.T) {
	ch := make(chan stream.Message, 10)
	logger := zerolog.Nop()
	coord := NewCoordinator(ch, logger)

	ctx := context.Background()
	id1, _ := coord.Initiate(ctx, pglogrepl.LSN(100))
	id2, _ := coord.Initiate(ctx, pglogrepl.LSN(200))

	if id1 == id2 {
		t.Error("expected different IDs for sequential initiations")
	}
	if id1 != "sentinel-1" || id2 != "sentinel-2" {
		t.Errorf("IDs = %q, %q, want sentinel-1, sentinel-2", id1, id2)
	}

	coord.Confirm(id1)
	coord.Confirm(id2)
}

func TestCoordinator_DoubleConfirm(t *testing.T) {
	ch := make(chan stream.Message, 10)
	logger := zerolog.Nop()
	coord := NewCoordinator(ch, logger)

	ctx := context.Background()
	id, _ := coord.Initiate(ctx, pglogrepl.LSN(100))
	coord.Confirm(id)
	coord.Confirm(id) // should not panic
}
