package config

import (
	"strings"
	"testing"
)

func TestDSN(t *testing.T) {
	tests := []struct {
		name string
		db   DatabaseConfig
		want string
	}{
		{
			name: "basic",
			db:   DatabaseConfig{Host: "localhost", Port: 5432, User: "postgres", Password: "secret", DBName: "mydb"},
			want: "postgres://postgres:secret@localhost:5432/mydb",
		},
		{
			name: "special chars in password",
			db:   DatabaseConfig{Host: "10.0.0.1", Port: 5433, User: "admin", Password: "p@ss:w/rd", DBName: "prod"},
			want: "postgres://admin:p%40ss%3Aw%2Frd@10.0.0.1:5433/prod",
		},
		{
			name: "empty password",
			db:   DatabaseConfig{Host: "localhost", Port: 5432, User: "postgres", Password: "", DBName: "test"},
			want: "postgres://postgres:@localhost:5432/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.db.DSN()
			if got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReplicationDSN(t *testing.T) {
	db := DatabaseConfig{Host: "localhost", Port: 5432, User: "postgres", Password: "secret", DBName: "mydb"}
	got := db.ReplicationDSN()
	if !strings.Contains(got, "replication=database") {
		t.Errorf("ReplicationDSN() = %q, missing replication=database", got)
	}
	if !strings.HasPrefix(got, "postgres://") {
		t.Errorf("ReplicationDSN() = %q, missing postgres:// prefix", got)
	}
}

func TestValidate_AllValid(t *testing.T) {
	cfg := Config{
		Source:      DatabaseConfig{Host: "src", DBName: "srcdb"},
		Dest:        DatabaseConfig{Host: "dst", DBName: "dstdb"},
		Replication: ReplicationConfig{SlotName: "slot", Publication: "pub"},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
	if cfg.Replication.OutputPlugin != "pgoutput" {
		t.Errorf("expected default output plugin pgoutput, got %s", cfg.Replication.OutputPlugin)
	}
	if cfg.Snapshot.Workers != 4 {
		t.Errorf("expected default workers 4, got %d", cfg.Snapshot.Workers)
	}
}

func TestValidate_MissingFields(t *testing.T) {
	cfg := Config{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for empty config")
	}

	errStr := err.Error()
	expected := []string{
		"source host is required",
		"source database name is required",
		"destination host is required",
		"destination database name is required",
		"replication slot name is required",
		"publication name is required",
	}
	for _, e := range expected {
		if !strings.Contains(errStr, e) {
			t.Errorf("Validate() error %q missing expected message: %q", errStr, e)
		}
	}
}

func TestValidate_DefaultsApplied(t *testing.T) {
	cfg := Config{
		Source:      DatabaseConfig{Host: "src", DBName: "srcdb"},
		Dest:        DatabaseConfig{Host: "dst", DBName: "dstdb"},
		Replication: ReplicationConfig{SlotName: "slot", Publication: "pub", OutputPlugin: ""},
		Snapshot:    SnapshotConfig{Workers: -1},
	}
	_ = cfg.Validate()
	if cfg.Replication.OutputPlugin != "pgoutput" {
		t.Errorf("expected default output plugin, got %q", cfg.Replication.OutputPlugin)
	}
	if cfg.Snapshot.Workers != 4 {
		t.Errorf("expected default workers 4, got %d", cfg.Snapshot.Workers)
	}
}

func TestValidate_PartialMissing(t *testing.T) {
	cfg := Config{
		Source:      DatabaseConfig{Host: "src"},
		Dest:        DatabaseConfig{Host: "dst", DBName: "dstdb"},
		Replication: ReplicationConfig{SlotName: "slot", Publication: "pub"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing source dbname")
	}
	if !strings.Contains(err.Error(), "source database name is required") {
		t.Errorf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "destination") {
		t.Errorf("should not have destination error: %v", err)
	}
}
