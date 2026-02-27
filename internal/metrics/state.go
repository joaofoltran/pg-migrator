package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

const (
	stateDir  = ".pgmigrator"
	stateFile = "state.json"
)

// StatePersister periodically writes the current Snapshot to a JSON file
// so that `pgmigrator status` can read it even when no pipeline is running.
type StatePersister struct {
	collector *Collector
	logger    zerolog.Logger
	path      string
	done      chan struct{}
}

// NewStatePersister creates a persister that writes to ~/.pgmigrator/state.json.
func NewStatePersister(collector *Collector, logger zerolog.Logger) (*StatePersister, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, stateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &StatePersister{
		collector: collector,
		logger:    logger.With().Str("component", "state-persister").Logger(),
		path:      filepath.Join(dir, stateFile),
		done:      make(chan struct{}),
	}, nil
}

// Start begins periodic state file writes every 2 seconds.
func (sp *StatePersister) Start() {
	go sp.loop()
}

// Stop halts the persister and writes a final snapshot.
func (sp *StatePersister) Stop() {
	select {
	case <-sp.done:
	default:
		close(sp.done)
	}
	sp.write() // Final write.
}

// Path returns the state file path.
func (sp *StatePersister) Path() string {
	return sp.path
}

func (sp *StatePersister) loop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-sp.done:
			return
		case <-ticker.C:
			sp.write()
		}
	}
}

func (sp *StatePersister) write() {
	snap := sp.collector.Snapshot()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		sp.logger.Err(err).Msg("marshal state")
		return
	}
	// Write to temp file then rename for atomicity.
	tmp := sp.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		sp.logger.Err(err).Msg("write state file")
		return
	}
	if err := os.Rename(tmp, sp.path); err != nil {
		sp.logger.Err(err).Msg("rename state file")
	}
}

// ReadStateFile reads the last-persisted Snapshot from ~/.pgmigrator/state.json.
func ReadStateFile() (*Snapshot, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, stateDir, stateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
