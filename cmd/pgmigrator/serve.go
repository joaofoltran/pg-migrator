package main

import (
	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/metrics"
	"github.com/jfoltran/pgmigrator/internal/server"
)

var servePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start standalone web UI server",
	Long: `Serve starts the pgmigrator web UI and API server.
It reads the last-known state from the state file and serves
the dashboard for monitoring. When a pipeline is running, it
shows live data via the metrics API.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create a collector that reads from state file for offline mode.
		collector := metrics.NewCollector(logger)
		defer collector.Close()

		// Try to load last state.
		if snap, err := metrics.ReadStateFile(); err == nil {
			collector.SetPhase(snap.Phase)
		}

		srv := server.New(collector, &cfg, logger)
		return srv.Start(cmd.Context(), servePort)
	},
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 7654, "HTTP server port")
	rootCmd.AddCommand(serveCmd)
}
