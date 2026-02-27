package main

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/config"
)

var (
	cfg    config.Config
	logger zerolog.Logger
)

var rootCmd = &cobra.Command{
	Use:   "pgmigrator",
	Short: "PostgreSQL online migration tool",
	Long: `pgmigrator is a middleman between source and destination PostgreSQL databases.
It owns the WAL stream, performs parallel COPY with consistent snapshots,
and supports zero-downtime switchover via sentinel markers.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger.
		switch cfg.Logging.Format {
		case "json":
			logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
		default:
			logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
				With().Timestamp().Logger()
		}

		level, err := zerolog.ParseLevel(cfg.Logging.Level)
		if err != nil {
			level = zerolog.InfoLevel
		}
		logger = logger.Level(level)

		return nil
	},
}

func init() {
	f := rootCmd.PersistentFlags()

	// Source database flags.
	f.StringVar(&cfg.Source.Host, "source-host", "localhost", "Source PostgreSQL host")
	f.Uint16Var(&cfg.Source.Port, "source-port", 5432, "Source PostgreSQL port")
	f.StringVar(&cfg.Source.User, "source-user", "postgres", "Source PostgreSQL user")
	f.StringVar(&cfg.Source.Password, "source-password", "", "Source PostgreSQL password")
	f.StringVar(&cfg.Source.DBName, "source-dbname", "", "Source database name")

	// Destination database flags.
	f.StringVar(&cfg.Dest.Host, "dest-host", "localhost", "Destination PostgreSQL host")
	f.Uint16Var(&cfg.Dest.Port, "dest-port", 5432, "Destination PostgreSQL port")
	f.StringVar(&cfg.Dest.User, "dest-user", "postgres", "Destination PostgreSQL user")
	f.StringVar(&cfg.Dest.Password, "dest-password", "", "Destination PostgreSQL password")
	f.StringVar(&cfg.Dest.DBName, "dest-dbname", "", "Destination database name")

	// Replication flags.
	f.StringVar(&cfg.Replication.SlotName, "slot", "pgmigrator", "Replication slot name")
	f.StringVar(&cfg.Replication.Publication, "publication", "pgmigrator_pub", "Publication name")
	f.StringVar(&cfg.Replication.OutputPlugin, "output-plugin", "pgoutput", "Logical decoding output plugin")
	f.StringVar(&cfg.Replication.OriginID, "origin-id", "", "Replication origin ID (for bidi loop detection)")

	// Snapshot flags.
	f.IntVar(&cfg.Snapshot.Workers, "copy-workers", 4, "Number of parallel COPY workers")

	// Logging flags.
	f.StringVar(&cfg.Logging.Level, "log-level", "info", "Log level (debug, info, warn, error)")
	f.StringVar(&cfg.Logging.Format, "log-format", "console", "Log format (console, json)")
}
