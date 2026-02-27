package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/pipeline"
)

var switchoverTimeout time.Duration

var switchoverCmd = &cobra.Command{
	Use:   "switchover",
	Short: "Initiate zero-downtime switchover via sentinel marker",
	Long: `Switchover injects a sentinel message into the replication
stream and waits for confirmation that it has been applied to the destination.
This confirms the destination is fully caught up and ready to serve traffic.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.Validate(); err != nil {
			return err
		}

		p := pipeline.New(&cfg, logger)
		defer p.Close()

		return p.RunSwitchover(cmd.Context(), switchoverTimeout)
	},
}

func init() {
	switchoverCmd.Flags().DurationVar(&switchoverTimeout, "timeout", 30*time.Second, "Maximum time to wait for sentinel confirmation")
	rootCmd.AddCommand(switchoverCmd)
}
