package main

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/metrics"
	"github.com/jfoltran/pgmigrator/internal/pipeline"
	"github.com/jfoltran/pgmigrator/internal/server"
	"github.com/jfoltran/pgmigrator/internal/tui"
)

var (
	cloneFollow  bool
	cloneResume  bool
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
4. With --follow, transitions to CDC streaming after the copy

Use --resume to continue an interrupted clone. This requires that the
replication slot from the original clone still exists on the source.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.Validate(); err != nil {
			return err
		}

		if cloneResume && !cloneFollow {
			return fmt.Errorf("--resume requires --follow (resume always transitions to CDC streaming)")
		}

		pipelineLogger := logger
		p := pipeline.New(&cfg, pipelineLogger)
		defer p.Close()

		if cloneTUI || cloneAPIPort > 0 {
			logWriter := metrics.NewLogWriter(p.Metrics)
			var newLogger zerolog.Logger
			if cloneTUI {
				newLogger = zerolog.New(logWriter).With().Timestamp().Logger()
			} else {
				newLogger = zerolog.New(zerolog.MultiLevelWriter(logOutput, logWriter)).With().Timestamp().Logger()
			}
			newLogger = newLogger.Level(logger.GetLevel())
			p.SetLogger(newLogger)
			pipelineLogger = newLogger
		}

		if cloneAPIPort > 0 {
			srv := server.New(p.Metrics, &cfg, pipelineLogger)
			srv.StartBackground(cmd.Context(), cloneAPIPort)
		}

		run := p.RunClone
		if cloneFollow {
			run = p.RunCloneAndFollow
		}
		if cloneResume {
			run = p.RunResumeCloneAndFollow
		}

		if cloneTUI {
			errCh := make(chan error, 1)
			go func() {
				errCh <- run(cmd.Context())
			}()
			return tui.Run(p.Metrics, errCh)
		}

		return run(cmd.Context())
	},
}

func init() {
	cloneCmd.Flags().BoolVar(&cloneFollow, "follow", false, "Continue with CDC streaming after initial copy")
	cloneCmd.Flags().BoolVar(&cloneResume, "resume", false, "Resume an interrupted clone (requires existing replication slot)")
	cloneCmd.Flags().IntVar(&cloneAPIPort, "api-port", 0, "Enable HTTP API on this port (0 = disabled)")
	cloneCmd.Flags().BoolVar(&cloneTUI, "tui", false, "Show terminal dashboard during migration")
	rootCmd.AddCommand(cloneCmd)
}
