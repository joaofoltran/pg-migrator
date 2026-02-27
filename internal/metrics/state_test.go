package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestStatePersister_WriteAndRead(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	c.SetPhase("streaming")
	c.RecordApplied(100, 50, 1024)

	// Create persister with temp directory.
	tmpDir := t.TempDir()
	sp := &StatePersister{
		collector: c,
		logger:    zerolog.Nop(),
		path:      filepath.Join(tmpDir, "state.json"),
		done:      make(chan struct{}),
	}

	sp.write()

	data, err := os.ReadFile(sp.path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if snap.Phase != "streaming" {
		t.Errorf("Phase = %q, want streaming", snap.Phase)
	}
	if snap.TotalRows != 50 {
		t.Errorf("TotalRows = %d, want 50", snap.TotalRows)
	}
}

func TestStatePersister_AtomicWrite(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")
	sp := &StatePersister{
		collector: c,
		logger:    zerolog.Nop(),
		path:      path,
		done:      make(chan struct{}),
	}

	sp.write()

	// Verify no .tmp file remains.
	tmpFile := path + ".tmp"
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("temporary file should not exist after write")
	}

	// Verify main file exists.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist: %v", err)
	}
}

func TestStatePersister_StartStop(t *testing.T) {
	c := NewCollector(zerolog.Nop())
	defer c.Close()

	tmpDir := t.TempDir()
	sp := &StatePersister{
		collector: c,
		logger:    zerolog.Nop(),
		path:      filepath.Join(tmpDir, "state.json"),
		done:      make(chan struct{}),
	}

	sp.Start()
	time.Sleep(100 * time.Millisecond)
	sp.Stop()

	// Double stop should not panic.
	sp.Stop()
}

func TestSnapshotJSON(t *testing.T) {
	snap := Snapshot{
		Timestamp: time.Now(),
		Phase:     "copy",
		Tables: []TableProgress{
			{Schema: "public", Name: "users", Status: TableCopied, Percent: 100},
		},
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if decoded.Phase != "copy" {
		t.Errorf("Phase = %q, want copy", decoded.Phase)
	}
	if len(decoded.Tables) != 1 {
		t.Fatalf("Tables count = %d, want 1", len(decoded.Tables))
	}
	if decoded.Tables[0].Status != TableCopied {
		t.Errorf("Table status = %q, want copied", decoded.Tables[0].Status)
	}
}
