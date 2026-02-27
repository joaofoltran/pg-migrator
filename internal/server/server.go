package server

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmigrator/internal/config"
	"github.com/jfoltran/pgmigrator/internal/metrics"
)

// Server is the HTTP server that serves the REST API, WebSocket endpoint,
// and embedded frontend static files.
type Server struct {
	collector *metrics.Collector
	cfg       *config.Config
	logger    zerolog.Logger
	hub       *Hub
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

	// Serve embedded frontend.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return fmt.Errorf("embed fs: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

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
