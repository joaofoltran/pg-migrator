package cluster

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.json")
	s := &Store{path: path}
	return s
}

func TestStoreAddAndGet(t *testing.T) {
	s := setupTestStore(t)

	c := Cluster{
		ID:   "prod",
		Name: "Production",
		Nodes: []Node{
			{ID: "primary", Name: "pg-primary", Host: "10.0.0.1", Port: 5432, Role: RolePrimary},
		},
	}

	if err := s.Add(c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := s.Get("prod")
	if !ok {
		t.Fatal("Get: cluster not found")
	}
	if got.Name != "Production" {
		t.Errorf("Name = %q, want %q", got.Name, "Production")
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("Nodes count = %d, want 1", len(got.Nodes))
	}
	if got.Nodes[0].Host != "10.0.0.1" {
		t.Errorf("Node host = %q, want %q", got.Nodes[0].Host, "10.0.0.1")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestStoreDuplicateAdd(t *testing.T) {
	s := setupTestStore(t)

	c := Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	}
	if err := s.Add(c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(c); err == nil {
		t.Fatal("expected error on duplicate add")
	}
}

func TestStoreList(t *testing.T) {
	s := setupTestStore(t)

	if list := s.List(); len(list) != 0 {
		t.Fatalf("List: expected empty, got %d", len(list))
	}

	for _, id := range []string{"a", "b", "c"} {
		s.Add(Cluster{
			ID:    id,
			Name:  id,
			Nodes: []Node{{ID: "n", Host: "h", Port: 5432}},
		})
	}

	if list := s.List(); len(list) != 3 {
		t.Fatalf("List: expected 3, got %d", len(list))
	}
}

func TestStoreUpdate(t *testing.T) {
	s := setupTestStore(t)

	c := Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	}
	s.Add(c)

	c.Name = "Production (updated)"
	if err := s.Update(c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := s.Get("prod")
	if got.Name != "Production (updated)" {
		t.Errorf("Name = %q, want %q", got.Name, "Production (updated)")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be preserved after update")
	}
}

func TestStoreUpdateNotFound(t *testing.T) {
	s := setupTestStore(t)
	err := s.Update(Cluster{ID: "nope"})
	if err == nil {
		t.Fatal("expected error on update of nonexistent cluster")
	}
}

func TestStoreRemove(t *testing.T) {
	s := setupTestStore(t)

	s.Add(Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	})

	if err := s.Remove("prod"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, ok := s.Get("prod"); ok {
		t.Fatal("cluster should be removed")
	}
}

func TestStoreRemoveNotFound(t *testing.T) {
	s := setupTestStore(t)
	if err := s.Remove("nope"); err == nil {
		t.Fatal("expected error on remove of nonexistent cluster")
	}
}

func TestStoreAddNode(t *testing.T) {
	s := setupTestStore(t)

	s.Add(Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "primary", Host: "h1", Port: 5432, Role: RolePrimary}},
	})

	n := Node{ID: "replica1", Name: "pg-replica1", Host: "10.0.0.2", Port: 5432, Role: RoleReplica}
	if err := s.AddNode("prod", n); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	c, _ := s.Get("prod")
	if len(c.Nodes) != 2 {
		t.Fatalf("Nodes count = %d, want 2", len(c.Nodes))
	}
}

func TestStoreAddNodeDuplicate(t *testing.T) {
	s := setupTestStore(t)

	s.Add(Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "primary", Host: "h1", Port: 5432}},
	})

	if err := s.AddNode("prod", Node{ID: "primary", Host: "h2", Port: 5432}); err == nil {
		t.Fatal("expected error on duplicate node add")
	}
}

func TestStoreRemoveNode(t *testing.T) {
	s := setupTestStore(t)

	s.Add(Cluster{
		ID:   "prod",
		Name: "Production",
		Nodes: []Node{
			{ID: "primary", Host: "h1", Port: 5432},
			{ID: "replica", Host: "h2", Port: 5432},
		},
	})

	if err := s.RemoveNode("prod", "replica"); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}

	c, _ := s.Get("prod")
	if len(c.Nodes) != 1 {
		t.Fatalf("Nodes count = %d, want 1", len(c.Nodes))
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.json")

	s1 := &Store{path: path}
	s1.Add(Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	})

	s2 := &Store{path: path}
	if err := s2.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	c, ok := s2.Get("prod")
	if !ok {
		t.Fatal("cluster not found after reload")
	}
	if c.Name != "Production" {
		t.Errorf("Name = %q, want %q", c.Name, "Production")
	}
}

func TestStoreFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.json")

	s := &Store{path: path}
	s.Add(Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestValidateCluster(t *testing.T) {
	tests := []struct {
		name    string
		cluster Cluster
		wantErr bool
	}{
		{
			name: "valid",
			cluster: Cluster{
				ID:    "prod",
				Name:  "Production",
				Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
			},
			wantErr: false,
		},
		{
			name:    "missing id",
			cluster: Cluster{Name: "x", Nodes: []Node{{ID: "n", Host: "h", Port: 5432}}},
			wantErr: true,
		},
		{
			name:    "missing name",
			cluster: Cluster{ID: "x", Nodes: []Node{{ID: "n", Host: "h", Port: 5432}}},
			wantErr: true,
		},
		{
			name:    "no nodes",
			cluster: Cluster{ID: "x", Name: "x"},
			wantErr: true,
		},
		{
			name:    "node missing host",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{{ID: "n", Port: 5432}}},
			wantErr: true,
		},
		{
			name:    "node missing port",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{{ID: "n", Host: "h"}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCluster(tt.cluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCluster() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
