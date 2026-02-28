package server

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jfoltran/migrator/internal/cluster"
	"github.com/jfoltran/migrator/internal/config"
	"github.com/jfoltran/migrator/internal/daemon"
	"github.com/jfoltran/migrator/internal/metrics"
)

// Server is the HTTP server that serves the REST API, WebSocket endpoint,
// and embedded frontend static files.
type Server struct {
	collector *metrics.Collector
	cfg       *config.Config
	logger    zerolog.Logger
	hub       *Hub
	jobs      *daemon.JobManager
	clusters  *cluster.Store
	srv       *http.Server
}

// New creates a new Server.
func New(collector *metrics.Collector, cfg *config.Config, logger zerolog.Logger) *Server {
	hub := newHub(collector, logger)
	return &Server{
		collector: collector,
		cfg:       cfg,
		logger:    logger.With().Str("component", "http-server").Logger(),
		hub:       hub,
	}
}

// SetJobManager attaches a job manager for daemon mode.
func (s *Server) SetJobManager(jm *daemon.JobManager) {
	s.jobs = jm
}

// SetClusterStore attaches a cluster store for multi-cluster management.
func (s *Server) SetClusterStore(cs *cluster.Store) {
	s.clusters = cs
}

// Start begins serving on the given port. It blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context, port int) error {
	h := &handlers{collector: s.collector, cfg: s.cfg}

	mux := http.NewServeMux()

	// API routes.
	mux.HandleFunc("GET /api/v1/status", h.status)
	mux.HandleFunc("GET /api/v1/tables", h.tables)
	mux.HandleFunc("GET /api/v1/config", h.configHandler)
	mux.HandleFunc("GET /api/v1/logs", h.logs)
	mux.HandleFunc("/api/v1/ws", s.hub.handleWS)

	// Job control routes (daemon mode).
	if s.jobs != nil {
		jh := &jobHandlers{jobs: s.jobs}
		mux.HandleFunc("POST /api/v1/jobs/clone", jh.submitClone)
		mux.HandleFunc("POST /api/v1/jobs/follow", jh.submitFollow)
		mux.HandleFunc("POST /api/v1/jobs/switchover", jh.submitSwitchover)
		mux.HandleFunc("POST /api/v1/jobs/stop", jh.stopJob)
		mux.HandleFunc("GET /api/v1/jobs/status", jh.jobStatus)
	}

	// Cluster management routes.
	if s.clusters != nil {
		ch := &clusterHandlers{store: s.clusters}
		mux.HandleFunc("GET /api/v1/clusters", ch.list)
		mux.HandleFunc("POST /api/v1/clusters", ch.add)
		mux.HandleFunc("GET /api/v1/clusters/{id}", ch.get)
		mux.HandleFunc("PUT /api/v1/clusters/{id}", ch.update)
		mux.HandleFunc("DELETE /api/v1/clusters/{id}", ch.remove)
		mux.HandleFunc("POST /api/v1/clusters/test-connection", ch.testConnection)
	}

	// Serve embedded frontend with SPA fallback.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return fmt.Errorf("embed fs: %w", err)
	}
	mux.Handle("/", spaHandler(http.FS(sub)))

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	// Start WebSocket hub.
	go s.hub.start(ctx)

	s.logger.Info().Int("port", port).Msg("starting HTTP server")

	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.srv.Close()
	case err := <-errCh:
		return err
	}
}

// StartBackground starts the server in a goroutine (non-blocking).
func (s *Server) StartBackground(ctx context.Context, port int) {
	go func() {
		if err := s.Start(ctx, port); err != nil {
			s.logger.Err(err).Msg("http server error")
		}
	}()
}

func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" && !strings.HasPrefix(path, "/api/") {
			f, err := fsys.Open(path)
			if err != nil {
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}
