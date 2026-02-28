package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jfoltran/pgmanager/internal/config"
	"github.com/jfoltran/pgmanager/internal/daemon"
)

type jobHandlers struct {
	jobs *daemon.JobManager
}

func (jh *jobHandlers) submitClone(w http.ResponseWriter, r *http.Request) {
	var payload daemon.ClonePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJobResponse(w, http.StatusBadRequest, daemon.JobResponse{
			Error: "invalid request body: " + err.Error(),
		})
		return
	}

	cfg := buildConfig(payload.SourceURI, payload.DestURI, payload.SlotName, payload.Publication, payload.Workers)
	if err := cfg.Validate(); err != nil {
		writeJobResponse(w, http.StatusBadRequest, daemon.JobResponse{
			Error: "invalid config: " + err.Error(),
		})
		return
	}

	if err := jh.jobs.RunClone(r.Context(), cfg, payload.Follow, payload.Resume); err != nil {
		writeJobResponse(w, http.StatusConflict, daemon.JobResponse{
			Error: err.Error(),
		})
		return
	}

	action := "clone"
	if payload.Resume {
		action = "resume clone + follow"
	} else if payload.Follow {
		action = "clone + follow"
	}
	writeJobResponse(w, http.StatusAccepted, daemon.JobResponse{
		OK:      true,
		Message: action + " started",
	})
}

func (jh *jobHandlers) submitFollow(w http.ResponseWriter, r *http.Request) {
	var payload daemon.FollowPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJobResponse(w, http.StatusBadRequest, daemon.JobResponse{
			Error: "invalid request body: " + err.Error(),
		})
		return
	}

	cfg := buildConfig(payload.SourceURI, payload.DestURI, payload.SlotName, payload.Publication, 0)
	if err := cfg.Validate(); err != nil {
		writeJobResponse(w, http.StatusBadRequest, daemon.JobResponse{
			Error: "invalid config: " + err.Error(),
		})
		return
	}

	if err := jh.jobs.RunFollow(r.Context(), cfg, payload.StartLSN); err != nil {
		writeJobResponse(w, http.StatusConflict, daemon.JobResponse{
			Error: err.Error(),
		})
		return
	}

	writeJobResponse(w, http.StatusAccepted, daemon.JobResponse{
		OK:      true,
		Message: "follow started",
	})
}

func (jh *jobHandlers) submitSwitchover(w http.ResponseWriter, r *http.Request) {
	var payload daemon.SwitchoverPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJobResponse(w, http.StatusBadRequest, daemon.JobResponse{
			Error: "invalid request body: " + err.Error(),
		})
		return
	}

	cfg := buildConfig(payload.SourceURI, payload.DestURI, payload.SlotName, payload.Publication, 0)
	if err := cfg.Validate(); err != nil {
		writeJobResponse(w, http.StatusBadRequest, daemon.JobResponse{
			Error: "invalid config: " + err.Error(),
		})
		return
	}

	timeout := 30 * time.Second
	if payload.TimeoutSec > 0 {
		timeout = time.Duration(payload.TimeoutSec) * time.Second
	}

	if err := jh.jobs.RunSwitchover(r.Context(), cfg, timeout); err != nil {
		writeJobResponse(w, http.StatusConflict, daemon.JobResponse{
			Error: err.Error(),
		})
		return
	}

	writeJobResponse(w, http.StatusAccepted, daemon.JobResponse{
		OK:      true,
		Message: "switchover started",
	})
}

func (jh *jobHandlers) stopJob(w http.ResponseWriter, r *http.Request) {
	if err := jh.jobs.StopJob(); err != nil {
		writeJobResponse(w, http.StatusConflict, daemon.JobResponse{
			Error: err.Error(),
		})
		return
	}
	writeJobResponse(w, http.StatusOK, daemon.JobResponse{
		OK:      true,
		Message: "job stop requested",
	})
}

func (jh *jobHandlers) jobStatus(w http.ResponseWriter, r *http.Request) {
	running := jh.jobs.IsRunning()
	resp := map[string]any{
		"running": running,
	}
	if !running {
		if err := jh.jobs.LastError(); err != nil {
			resp["last_error"] = err.Error()
		}
	}
	writeJSON(w, resp)
}

func writeJobResponse(w http.ResponseWriter, status int, resp daemon.JobResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

func buildConfig(sourceURI, destURI, slotName, publication string, workers int) *config.Config {
	cfg := &config.Config{}

	if sourceURI != "" {
		cfg.Source.ParseURI(sourceURI) //nolint:errcheck
	}
	if destURI != "" {
		cfg.Dest.ParseURI(destURI) //nolint:errcheck
	}

	applyConfigDefaults(&cfg.Source)
	applyConfigDefaults(&cfg.Dest)

	if slotName != "" {
		cfg.Replication.SlotName = slotName
	} else {
		cfg.Replication.SlotName = "pgmanager"
	}
	if publication != "" {
		cfg.Replication.Publication = publication
	} else {
		cfg.Replication.Publication = "pgmanager_pub"
	}
	cfg.Replication.OutputPlugin = "pgoutput"

	if workers > 0 {
		cfg.Snapshot.Workers = workers
	} else {
		cfg.Snapshot.Workers = 4
	}

	return cfg
}

func applyConfigDefaults(d *config.DatabaseConfig) {
	if d.Host == "" {
		d.Host = "localhost"
	}
	if d.Port == 0 {
		d.Port = 5432
	}
	if d.User == "" {
		d.User = "postgres"
	}
}
