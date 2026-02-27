package main

import (
	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/pipeline"
	"github.com/jfoltran/pgmigrator/internal/server"
	"github.com/jfoltran/pgmigrator/internal/tui"
)

var (
	cloneFollow  bool
	cloneAPIPort int
	cloneTUI     bool
)

var cloneCmd = &cobra.Command{
	Use:   "clone",
	Short: "Copy schema and data from source to destination",
	Long: `Clone performs a full copy of the source database to the destination:
1. Dumps and applies schema (DDL)
2. Creates a replication slot for a consistent snapshot
3. Copies all tables in parallel using the snapshot
4. With --follow, transitions to CDC streaming after the copy`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.Validate(); err != nil {
			return err
		}

		p := pipeline.New(&cfg, logger)
		defer p.Close()

		// Start API server if port is configured.
		if cloneAPIPort > 0 {
			srv := server.New(p.Metrics, &cfg, logger)
			srv.StartBackground(cmd.Context(), cloneAPIPort)
		}

		// Run with TUI if requested.
		if cloneTUI {
			errCh := make(chan error, 1)
			go func() {
				if cloneFollow {
					errCh <- p.RunCloneAndFollow(cmd.Context())
				} else {
					errCh <- p.RunClone(cmd.Context())
				}
			}()

			if err := tui.Run(p.Metrics); err != nil {
				return err
			}
			return <-errCh
		}

		if cloneFollow {
			return p.RunCloneAndFollow(cmd.Context())
		}
		return p.RunClone(cmd.Context())
	},
}

func init() {
	cloneCmd.Flags().BoolVar(&cloneFollow, "follow", false, "Continue with CDC streaming after initial copy")
	cloneCmd.Flags().IntVar(&cloneAPIPort, "api-port", 0, "Enable HTTP API on this port (0 = disabled)")
	cloneCmd.Flags().BoolVar(&cloneTUI, "tui", false, "Show terminal dashboard during migration")
	rootCmd.AddCommand(cloneCmd)
}
