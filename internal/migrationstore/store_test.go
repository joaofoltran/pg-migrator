package migrationstore

import (
	"strings"
	"testing"
)

func TestValidateMigration(t *testing.T) {
	valid := Migration{
		ID:              "mig-1",
		Name:            "prod migration",
		SourceClusterID: "src-cluster",
		DestClusterID:   "dst-cluster",
		SourceNodeID:    "src-node",
		DestNodeID:      "dst-node",
		Mode:            ModeCloneOnly,
	}

	t.Run("valid clone_only", func(t *testing.T) {
		m := valid
		if err := ValidateMigration(m); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid clone_and_follow", func(t *testing.T) {
		m := valid
		m.Mode = ModeCloneAndFollow
		if err := ValidateMigration(m); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid clone_follow_switchover", func(t *testing.T) {
		m := valid
		m.Mode = ModeCloneFollowSwitch
		if err := ValidateMigration(m); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		m := valid
		m.ID = ""
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "migration id is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		m := valid
		m.Name = ""
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "migration name is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing source cluster", func(t *testing.T) {
		m := valid
		m.SourceClusterID = ""
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "source cluster is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing dest cluster", func(t *testing.T) {
		m := valid
		m.DestClusterID = ""
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "destination cluster is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing source node", func(t *testing.T) {
		m := valid
		m.SourceNodeID = ""
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "source node is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing dest node", func(t *testing.T) {
		m := valid
		m.DestNodeID = ""
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "destination node is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("same source and dest node in same cluster", func(t *testing.T) {
		m := valid
		m.SourceClusterID = "cluster-1"
		m.DestClusterID = "cluster-1"
		m.SourceNodeID = "node-1"
		m.DestNodeID = "node-1"
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "source and destination cannot be the same node") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("same cluster different nodes is ok", func(t *testing.T) {
		m := valid
		m.SourceClusterID = "cluster-1"
		m.DestClusterID = "cluster-1"
		m.SourceNodeID = "node-1"
		m.DestNodeID = "node-2"
		if err := ValidateMigration(m); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid mode", func(t *testing.T) {
		m := valid
		m.Mode = "invalid_mode"
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "invalid mode") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty mode", func(t *testing.T) {
		m := valid
		m.Mode = ""
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "invalid mode") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("multiple errors", func(t *testing.T) {
		m := Migration{}
		err := ValidateMigration(m)
		if err == nil {
			t.Fatal("expected error")
		}
		errStr := err.Error()
		for _, want := range []string{
			"migration id is required",
			"migration name is required",
			"source cluster is required",
			"destination cluster is required",
			"source node is required",
			"destination node is required",
			"invalid mode",
		} {
			if !strings.Contains(errStr, want) {
				t.Errorf("error %q missing expected message: %q", errStr, want)
			}
		}
	})
}

func TestModeConstants(t *testing.T) {
	if ModeCloneOnly != "clone_only" {
		t.Errorf("ModeCloneOnly = %q", ModeCloneOnly)
	}
	if ModeCloneAndFollow != "clone_and_follow" {
		t.Errorf("ModeCloneAndFollow = %q", ModeCloneAndFollow)
	}
	if ModeCloneFollowSwitch != "clone_follow_switchover" {
		t.Errorf("ModeCloneFollowSwitch = %q", ModeCloneFollowSwitch)
	}
}

func TestStatusConstants(t *testing.T) {
	statuses := map[Status]string{
		StatusCreated:    "created",
		StatusRunning:    "running",
		StatusStreaming:  "streaming",
		StatusSwitchover: "switchover",
		StatusCompleted:  "completed",
		StatusFailed:     "failed",
		StatusStopped:    "stopped",
	}
	for s, want := range statuses {
		if string(s) != want {
			t.Errorf("Status %q != %q", s, want)
		}
	}
}
