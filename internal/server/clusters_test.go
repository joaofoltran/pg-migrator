package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jfoltran/migrator/internal/cluster"
)

func TestClusterHandlersCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.json")
	os.WriteFile(path, []byte(`{"clusters":[]}`), 0o600)

	// We need a store that uses our temp path. Since NewStore uses DataDir,
	// we'll set HOME to our temp dir.
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.MkdirAll(filepath.Join(tmpHome, ".migrator"), 0o755)
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	store, err := cluster.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	ch := &clusterHandlers{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/clusters", ch.list)
	mux.HandleFunc("POST /api/v1/clusters", ch.add)
	mux.HandleFunc("GET /api/v1/clusters/{id}", ch.get)
	mux.HandleFunc("PUT /api/v1/clusters/{id}", ch.update)
	mux.HandleFunc("DELETE /api/v1/clusters/{id}", ch.remove)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// List empty.
	resp, _ := http.Get(srv.URL + "/api/v1/clusters")
	var listed []cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(listed))
	}

	// Add cluster.
	body := `{"id":"prod","name":"Production","nodes":[{"id":"primary","host":"10.0.0.1","port":5432,"role":"primary"}]}`
	resp, _ = http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d", resp.StatusCode)
	}
	var added cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&added)
	resp.Body.Close()
	if added.ID != "prod" {
		t.Errorf("add: ID = %q, want %q", added.ID, "prod")
	}
	if added.CreatedAt.IsZero() {
		t.Error("add: CreatedAt should not be zero")
	}

	// Get cluster.
	resp, _ = http.Get(srv.URL + "/api/v1/clusters/prod")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}
	var got cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Name != "Production" {
		t.Errorf("get: Name = %q, want %q", got.Name, "Production")
	}

	// Update cluster.
	updateBody := `{"name":"Production (v2)","nodes":[{"id":"primary","host":"10.0.0.2","port":5432,"role":"primary"}]}`
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/clusters/prod", bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", resp.StatusCode)
	}
	var updated cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()
	if updated.Name != "Production (v2)" {
		t.Errorf("update: Name = %q, want %q", updated.Name, "Production (v2)")
	}

	// List after add.
	resp, _ = http.Get(srv.URL + "/api/v1/clusters")
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(listed))
	}

	// Delete cluster.
	req, _ = http.NewRequest("DELETE", srv.URL+"/api/v1/clusters/prod", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
	}

	// Get deleted → 404.
	resp, _ = http.Get(srv.URL + "/api/v1/clusters/prod")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestClusterHandlersValidation(t *testing.T) {
	tmpHome := t.TempDir()
	os.MkdirAll(filepath.Join(tmpHome, ".migrator"), 0o755)
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	store, _ := cluster.NewStore()
	ch := &clusterHandlers{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/clusters", ch.add)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Missing required fields.
	body := `{"id":"","name":"","nodes":[]}`
	resp, _ := http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Duplicate add.
	body = `{"id":"x","name":"X","nodes":[{"id":"n","host":"h","port":5432}]}`
	http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	resp, _ = http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
