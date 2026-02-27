package server

import (
	"encoding/json"
	"net/http"

	"github.com/jfoltran/pgmigrator/internal/config"
	"github.com/jfoltran/pgmigrator/internal/metrics"
)

type handlers struct {
	collector *metrics.Collector
	cfg       *config.Config
}

func (h *handlers) status(w http.ResponseWriter, r *http.Request) {
	snap := h.collector.Snapshot()
	writeJSON(w, snap)
}

func (h *handlers) tables(w http.ResponseWriter, r *http.Request) {
	snap := h.collector.Snapshot()
	writeJSON(w, snap.Tables)
}

func (h *handlers) configHandler(w http.ResponseWriter, r *http.Request) {
	if h.cfg == nil {
		writeJSON(w, map[string]string{"error": "no config available"})
		return
	}
	// Redact passwords.
	redacted := struct {
		Source      redactedDB         `json:"source"`
		Dest        redactedDB         `json:"dest"`
		Replication config.ReplicationConfig `json:"replication"`
		Snapshot    config.SnapshotConfig    `json:"snapshot"`
	}{
		Source:      redactDB(h.cfg.Source),
		Dest:        redactDB(h.cfg.Dest),
		Replication: h.cfg.Replication,
		Snapshot:    h.cfg.Snapshot,
	}
	writeJSON(w, redacted)
}

func (h *handlers) logs(w http.ResponseWriter, r *http.Request) {
	entries := h.collector.Logs()
	writeJSON(w, entries)
}

type redactedDB struct {
	Host   string `json:"host"`
	Port   uint16 `json:"port"`
	User   string `json:"user"`
	DBName string `json:"dbname"`
}

func redactDB(d config.DatabaseConfig) redactedDB {
	return redactedDB{
		Host:   d.Host,
		Port:   d.Port,
		User:   d.User,
		DBName: d.DBName,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
