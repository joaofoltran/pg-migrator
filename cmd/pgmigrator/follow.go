package main

import (
	"github.com/jackc/pglogrepl"
	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/pipeline"
	"github.com/jfoltran/pgmigrator/internal/server"
	"github.com/jfoltran/pgmigrator/internal/tui"
)

var (
	followStartLSN string
	followAPIPort  int
	followTUI      bool
)

var followCmd = &cobra.Command{
	Use:   "follow",
	Short: "Stream CDC changes from source to destination",
	Long: `Follow starts consuming the WAL stream from the replication slot and
applies changes to the destination database in real-time.
The replication slot must already exist (created by a previous clone).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.Validate(); err != nil {
			return err
		}

		var startLSN pglogrepl.LSN
		if followStartLSN != "" {
			var err error
			startLSN, err = pglogrepl.ParseLSN(followStartLSN)
			if err != nil {
				return err
			}
		}

		p := pipeline.New(&cfg, logger)
		defer p.Close()

		// Start API server if port is configured.
		if followAPIPort > 0 {
			srv := server.New(p.Metrics, &cfg, logger)
			srv.StartBackground(cmd.Context(), followAPIPort)
		}

		// Run with TUI if requested.
		if followTUI {
			errCh := make(chan error, 1)
			go func() {
				errCh <- p.RunFollow(cmd.Context(), startLSN)
			}()

			if err := tui.Run(p.Metrics); err != nil {
				return err
			}
			return <-errCh
		}

		return p.RunFollow(cmd.Context(), startLSN)
	},
}

func init() {
	followCmd.Flags().StringVar(&followStartLSN, "start-lsn", "", "LSN to start streaming from (e.g. 0/1234ABC)")
	followCmd.Flags().IntVar(&followAPIPort, "api-port", 0, "Enable HTTP API on this port (0 = disabled)")
	followCmd.Flags().BoolVar(&followTUI, "tui", false, "Show terminal dashboard during streaming")
	rootCmd.AddCommand(followCmd)
}
