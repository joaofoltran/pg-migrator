package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jfoltran/pgmanager/internal/config"
)

func TestBuildConfig(t *testing.T) {
	t.Run("full params", func(t *testing.T) {
		cfg := buildConfig(
			"postgres://user:pass@src:5432/srcdb",
			"postgres://user:pass@dst:5432/dstdb",
			"myslot", "mypub", 8,
		)
		if cfg.Source.Host != "src" {
			t.Errorf("Source.Host = %q, want src", cfg.Source.Host)
		}
		if cfg.Dest.Host != "dst" {
			t.Errorf("Dest.Host = %q, want dst", cfg.Dest.Host)
		}
		if cfg.Replication.SlotName != "myslot" {
			t.Errorf("SlotName = %q, want myslot", cfg.Replication.SlotName)
		}
		if cfg.Replication.Publication != "mypub" {
			t.Errorf("Publication = %q, want mypub", cfg.Replication.Publication)
		}
		if cfg.Replication.OutputPlugin != "pgoutput" {
			t.Errorf("OutputPlugin = %q, want pgoutput", cfg.Replication.OutputPlugin)
		}
		if cfg.Snapshot.Workers != 8 {
			t.Errorf("Workers = %d, want 8", cfg.Snapshot.Workers)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		cfg := buildConfig("", "", "", "", 0)
		if cfg.Replication.SlotName != "pgmanager" {
			t.Errorf("SlotName = %q, want pgmanager", cfg.Replication.SlotName)
		}
		if cfg.Replication.Publication != "pgmanager_pub" {
			t.Errorf("Publication = %q, want pgmanager_pub", cfg.Replication.Publication)
		}
		if cfg.Snapshot.Workers != 4 {
			t.Errorf("Workers = %d, want 4", cfg.Snapshot.Workers)
		}
	})

	t.Run("empty URIs get host defaults", func(t *testing.T) {
		cfg := buildConfig("", "", "s", "p", 1)
		if cfg.Source.Host != "localhost" {
			t.Errorf("Source.Host = %q, want localhost", cfg.Source.Host)
		}
		if cfg.Source.Port != 5432 {
			t.Errorf("Source.Port = %d, want 5432", cfg.Source.Port)
		}
		if cfg.Source.User != "postgres" {
			t.Errorf("Source.User = %q, want postgres", cfg.Source.User)
		}
		if cfg.Dest.Host != "localhost" {
			t.Errorf("Dest.Host = %q, want localhost", cfg.Dest.Host)
		}
	})
}

func TestApplyConfigDefaults(t *testing.T) {
	t.Run("all empty", func(t *testing.T) {
		d := &config.DatabaseConfig{}
		applyConfigDefaults(d)
		if d.Host != "localhost" {
			t.Errorf("Host = %q, want localhost", d.Host)
		}
		if d.Port != 5432 {
			t.Errorf("Port = %d, want 5432", d.Port)
		}
		if d.User != "postgres" {
			t.Errorf("User = %q, want postgres", d.User)
		}
	})

	t.Run("no overwrite", func(t *testing.T) {
		d := &config.DatabaseConfig{Host: "custom", Port: 5433, User: "admin"}
		applyConfigDefaults(d)
		if d.Host != "custom" {
			t.Errorf("Host = %q, want custom", d.Host)
		}
		if d.Port != 5433 {
			t.Errorf("Port = %d, want 5433", d.Port)
		}
		if d.User != "admin" {
			t.Errorf("User = %q, want admin", d.User)
		}
	})
}

func TestRedactDB(t *testing.T) {
	db := config.DatabaseConfig{
		Host:     "secret-host.internal",
		Port:     5432,
		User:     "admin",
		Password: "super-secret-password",
		DBName:   "prod",
	}
	r := redactDB(db)
	if r.Host != "secret-host.internal" {
		t.Errorf("Host = %q", r.Host)
	}
	if r.Port != 5432 {
		t.Errorf("Port = %d", r.Port)
	}
	if r.User != "admin" {
		t.Errorf("User = %q", r.User)
	}
	if r.DBName != "prod" {
		t.Errorf("DBName = %q", r.DBName)
	}

	out, _ := json.Marshal(r)
	if containsSimple(string(out), "super-secret-password") {
		t.Error("redacted output should not contain password")
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	writeJSON(rec, data)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cors := rec.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("CORS = %q, want *", cors)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("got[key] = %q, want value", got["key"])
	}
}

func TestWriteJobResponse(t *testing.T) {
	rec := httptest.NewRecorder()

	type jobResp struct {
		OK      bool   `json:"ok"`
		Message string `json:"message,omitempty"`
		Error   string `json:"error,omitempty"`
	}

	writeJSON(rec, jobResp{OK: true, Message: "test"})

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	var got jobResp
	json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.OK {
		t.Error("expected OK=true")
	}
	if got.Message != "test" {
		t.Errorf("Message = %q", got.Message)
	}
}

func TestSPAHandler(t *testing.T) {
	handler := spaHandler(http.Dir("testdata"))

	t.Run("api paths pass through", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	})

	t.Run("root path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	})
}
