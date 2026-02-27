package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/metrics"
	"github.com/jfoltran/pgmigrator/internal/tui"
)

var tuiAPIAddr string

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch terminal dashboard",
	Long: `TUI starts a Bubble Tea terminal dashboard for monitoring a running
pgmigrator instance. It connects to the API endpoint of a running pipeline.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		collector := metrics.NewCollector(logger)
		defer collector.Close()

		// Poll the remote API and feed into the local collector.
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		go pollRemote(ctx, tuiAPIAddr, collector)

		return tui.Run(collector, nil)
	},
}

func init() {
	tuiCmd.Flags().StringVar(&tuiAPIAddr, "api-addr", "http://localhost:7654", "Address of pgmigrator API")
	rootCmd.AddCommand(tuiCmd)
}

func pollRemote(ctx context.Context, addr string, collector *metrics.Collector) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap, err := fetchStatus(client, addr)
			if err != nil {
				collector.RecordError(fmt.Errorf("api fetch: %w", err))
				continue
			}
			// Update local collector from remote snapshot.
			collector.SetPhase(snap.Phase)
			collector.SetTables(snap.Tables)
		}
	}
}

func fetchStatus(client *http.Client, addr string) (*metrics.Snapshot, error) {
	resp, err := client.Get(addr + "/api/v1/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var snap metrics.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
