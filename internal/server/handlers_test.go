package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jfoltran/migrator/internal/config"
	"github.com/jfoltran/migrator/internal/metrics"
)

func TestHandlerStatus(t *testing.T) {
	c := metrics.NewCollector(zerolog.Nop())
	defer c.Close()
	c.SetPhase("streaming")

	h := &handlers{collector: c}
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rec := httptest.NewRecorder()

	h.status(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var snap metrics.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if snap.Phase != "streaming" {
		t.Errorf("Phase = %q, want streaming", snap.Phase)
	}
}

func TestHandlerTables(t *testing.T) {
	c := metrics.NewCollector(zerolog.Nop())
	defer c.Close()
	c.SetTables([]metrics.TableProgress{
		{Schema: "public", Name: "users", Status: metrics.TableCopied},
	})

	h := &handlers{collector: c}
	req := httptest.NewRequest("GET", "/api/v1/tables", nil)
	rec := httptest.NewRecorder()

	h.tables(rec, req)

	var tables []metrics.TableProgress
	if err := json.Unmarshal(rec.Body.Bytes(), &tables); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}
	if tables[0].Name != "users" {
		t.Errorf("table name = %q, want users", tables[0].Name)
	}
}

func TestHandlerConfig(t *testing.T) {
	c := metrics.NewCollector(zerolog.Nop())
	defer c.Close()

	cfg := &config.Config{
		Source: config.DatabaseConfig{Host: "src", Port: 5432, User: "postgres", Password: "secret123", DBName: "mydb"},
		Dest:   config.DatabaseConfig{Host: "dst", Port: 5432, User: "postgres", Password: "dest_secret", DBName: "dstdb"},
	}

	h := &handlers{collector: c, cfg: cfg}
	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	rec := httptest.NewRecorder()

	h.configHandler(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want 200", rec.Code)
	}

	// Passwords should be redacted (not present in output).
	if contains(body, "secret123") || contains(body, "dest_secret") {
		t.Error("response should not contain passwords")
	}
	if !contains(body, "src") || !contains(body, "dst") {
		t.Error("response should contain host names")
	}
}

func TestHandlerConfigNil(t *testing.T) {
	c := metrics.NewCollector(zerolog.Nop())
	defer c.Close()

	h := &handlers{collector: c, cfg: nil}
	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	rec := httptest.NewRecorder()

	h.configHandler(rec, req)

	if !contains(rec.Body.String(), "no config available") {
		t.Error("expected 'no config available' error message")
	}
}

func TestHandlerLogs(t *testing.T) {
	c := metrics.NewCollector(zerolog.Nop())
	defer c.Close()

	c.AddLog(metrics.LogEntry{Level: "info", Message: "test log"})

	h := &handlers{collector: c}
	req := httptest.NewRequest("GET", "/api/v1/logs", nil)
	rec := httptest.NewRecorder()

	h.logs(rec, req)

	var logs []metrics.LogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &logs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != "test log" {
		t.Errorf("log message = %q, want 'test log'", logs[0].Message)
	}
}

func TestHandlerCORS(t *testing.T) {
	c := metrics.NewCollector(zerolog.Nop())
	defer c.Close()

	h := &handlers{collector: c}
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rec := httptest.NewRecorder()

	h.status(rec, req)

	cors := rec.Header().Get("Access-Control-Allow-Origin")
	if cors != "*" {
		t.Errorf("CORS header = %q, want *", cors)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != "" && s != substr && // avoid trivial matches
		json.Valid([]byte(s)) && // ensure valid json
		len(s) >= len(substr) &&
		containsSimple(s, substr)
}

func containsSimple(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
