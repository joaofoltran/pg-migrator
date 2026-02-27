package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/rs/zerolog"
)

func TestCollector_PhaseTracking(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	c.SetPhase("connecting")
	snap := c.Snapshot()
	if snap.Phase != "connecting" {
		t.Errorf("Phase = %q, want connecting", snap.Phase)
	}

	c.SetPhase("streaming")
	snap = c.Snapshot()
	if snap.Phase != "streaming" {
		t.Errorf("Phase = %q, want streaming", snap.Phase)
	}
}

func TestCollector_TableLifecycle(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	tables := []TableProgress{
		{Schema: "public", Name: "users", RowsTotal: 1000, SizeBytes: 4096},
		{Schema: "public", Name: "orders", RowsTotal: 5000, SizeBytes: 20480},
	}
	c.SetTables(tables)

	snap := c.Snapshot()
	if snap.TablesTotal != 2 {
		t.Errorf("TablesTotal = %d, want 2", snap.TablesTotal)
	}
	if snap.TablesCopied != 0 {
		t.Errorf("TablesCopied = %d, want 0", snap.TablesCopied)
	}

	c.TableStarted("public", "users")
	snap = c.Snapshot()
	found := false
	for _, tp := range snap.Tables {
		if tp.Name == "users" && tp.Status == TableCopying {
			found = true
		}
	}
	if !found {
		t.Error("users table should be in copying state")
	}

	c.TableDone("public", "users", 1000)
	snap = c.Snapshot()
	if snap.TablesCopied != 1 {
		t.Errorf("TablesCopied = %d, want 1", snap.TablesCopied)
	}
	for _, tp := range snap.Tables {
		if tp.Name == "users" {
			if tp.Status != TableCopied {
				t.Errorf("users status = %s, want copied", tp.Status)
			}
			if tp.Percent != 100 {
				t.Errorf("users percent = %.1f, want 100", tp.Percent)
			}
		}
	}

	c.TableStreaming("public", "users")
	snap = c.Snapshot()
	for _, tp := range snap.Tables {
		if tp.Name == "users" {
			if tp.Status != TableStreaming {
				t.Errorf("users status = %s, want streaming", tp.Status)
			}
		}
	}
}

func TestCollector_LSNTracking(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	c.RecordApplied(pglogrepl.LSN(100), 10, 1024)
	c.RecordConfirmedLSN(pglogrepl.LSN(90))
	c.RecordLatestLSN(pglogrepl.LSN(200))

	snap := c.Snapshot()
	if snap.AppliedLSN != "0/64" {
		t.Errorf("AppliedLSN = %q, want 0/64", snap.AppliedLSN)
	}
	if snap.LagBytes == 0 {
		t.Error("expected non-zero lag bytes")
	}
}

func TestCollector_ErrorTracking(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	c.RecordError(nil)
	snap := c.Snapshot()
	if snap.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", snap.ErrorCount)
	}

	c.RecordError(fmt.Errorf("test error"))
	snap = c.Snapshot()
	if snap.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d, want 2", snap.ErrorCount)
	}
	if snap.LastError != "test error" {
		t.Errorf("LastError = %q, want 'test error'", snap.LastError)
	}
}

func TestCollector_TotalCounters(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	c.RecordApplied(pglogrepl.LSN(100), 50, 2048)
	c.RecordApplied(pglogrepl.LSN(200), 30, 1024)

	snap := c.Snapshot()
	if snap.TotalRows != 80 {
		t.Errorf("TotalRows = %d, want 80", snap.TotalRows)
	}
	if snap.TotalBytes != 3072 {
		t.Errorf("TotalBytes = %d, want 3072", snap.TotalBytes)
	}
}

func TestCollector_LogBuffer(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	for i := 0; i < 10; i++ {
		c.AddLog(LogEntry{
			Time:    time.Now(),
			Level:   "info",
			Message: fmt.Sprintf("log %d", i),
		})
	}

	logs := c.Logs()
	if len(logs) != 10 {
		t.Errorf("expected 10 logs, got %d", len(logs))
	}
}

func TestCollector_LogBufferEviction(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	for i := 0; i < 600; i++ {
		c.AddLog(LogEntry{
			Time:    time.Now(),
			Level:   "info",
			Message: fmt.Sprintf("log %d", i),
		})
	}

	logs := c.Logs()
	if len(logs) > 500 {
		t.Errorf("log buffer should not exceed capacity, got %d", len(logs))
	}
}

func TestCollector_SubscribeUnsubscribe(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	ch := c.Subscribe()
	c.Unsubscribe(ch)

	// Should not panic or deadlock.
	c.SetPhase("test")
}

func TestCollector_UpdateTableProgress(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	tables := []TableProgress{
		{Schema: "public", Name: "users", RowsTotal: 1000, SizeBytes: 4096},
	}
	c.SetTables(tables)
	c.TableStarted("public", "users")
	c.UpdateTableProgress("public", "users", 500, 2048)

	snap := c.Snapshot()
	for _, tp := range snap.Tables {
		if tp.Name == "users" {
			if tp.RowsCopied != 500 {
				t.Errorf("RowsCopied = %d, want 500", tp.RowsCopied)
			}
			if tp.Percent != 50 {
				t.Errorf("Percent = %.1f, want 50", tp.Percent)
			}
		}
	}
}

func TestCollector_Elapsed(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	c.SetPhase("copy")
	time.Sleep(50 * time.Millisecond)
	snap := c.Snapshot()
	if snap.ElapsedSec < 0.04 {
		t.Errorf("ElapsedSec = %f, expected > 0.04", snap.ElapsedSec)
	}
}

func TestSlidingWindow_Rate(t *testing.T) {
	w := newSlidingWindow(5 * time.Second)
	now := time.Now()

	w.Add(now.Add(-3*time.Second), 30)
	w.Add(now.Add(-2*time.Second), 20)
	w.Add(now.Add(-1*time.Second), 10)

	rate := w.Rate()
	if rate <= 0 {
		t.Errorf("Rate() = %f, want > 0", rate)
	}
}

func TestSlidingWindow_Eviction(t *testing.T) {
	w := newSlidingWindow(100 * time.Millisecond)
	now := time.Now()

	w.Add(now.Add(-200*time.Millisecond), 100)
	w.Add(now, 50)

	rate := w.Rate()
	// The old entry should be evicted, leaving only the 50 entry.
	if rate <= 0 {
		t.Errorf("Rate() = %f, want > 0", rate)
	}
}

func TestSlidingWindow_Empty(t *testing.T) {
	w := newSlidingWindow(time.Second)
	if r := w.Rate(); r != 0 {
		t.Errorf("Rate() on empty window = %f, want 0", r)
	}
}
