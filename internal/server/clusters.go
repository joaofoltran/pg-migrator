package server

import (
	"encoding/json"
	"net/http"

	"github.com/jfoltran/migrator/internal/cluster"
)

type clusterHandlers struct {
	store *cluster.Store
}

func (ch *clusterHandlers) list(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, ch.store.List())
}

func (ch *clusterHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	c, ok := ch.store.Get(id)
	if !ok {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

type addClusterRequest struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Nodes []cluster.Node   `json:"nodes"`
	Tags  []string         `json:"tags,omitempty"`
}

func (ch *clusterHandlers) add(w http.ResponseWriter, r *http.Request) {
	var req addClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	c := cluster.Cluster{
		ID:    req.ID,
		Name:  req.Name,
		Nodes: req.Nodes,
		Tags:  req.Tags,
	}

	if err := cluster.ValidateCluster(c); err != nil {
		http.Error(w, "validation: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := ch.store.Add(c); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	got, _ := ch.store.Get(c.ID)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, got)
}

func (ch *clusterHandlers) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	var req addClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	c := cluster.Cluster{
		ID:    id,
		Name:  req.Name,
		Nodes: req.Nodes,
		Tags:  req.Tags,
	}

	if err := cluster.ValidateCluster(c); err != nil {
		http.Error(w, "validation: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := ch.store.Update(c); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	got, _ := ch.store.Get(id)
	writeJSON(w, got)
}

func (ch *clusterHandlers) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	if err := ch.store.Remove(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ch *clusterHandlers) testConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DSN string `json:"dsn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.DSN == "" {
		http.Error(w, "dsn is required", http.StatusBadRequest)
		return
	}

	result := cluster.TestConnection(r.Context(), req.DSN)
	writeJSON(w, result)
}

