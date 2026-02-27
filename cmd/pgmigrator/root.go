package main

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/config"
)

var (
	cfg       config.Config
	logger    zerolog.Logger
	logOutput io.Writer
	sourceURI string
	destURI   string
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
		if sourceURI != "" {
			clean := config.DatabaseConfig{}
			copyExplicitFlags(cmd, "source", &cfg.Source, &clean)
			cfg.Source = clean
			if err := cfg.Source.ParseURI(sourceURI); err != nil {
				return err
			}
			applyExplicitFlags(cmd, "source", &cfg.Source)
		}
		if destURI != "" {
			clean := config.DatabaseConfig{}
			copyExplicitFlags(cmd, "dest", &cfg.Dest, &clean)
			cfg.Dest = clean
			if err := cfg.Dest.ParseURI(destURI); err != nil {
				return err
			}
			applyExplicitFlags(cmd, "dest", &cfg.Dest)
		}
		applyDefaults(&cfg.Source)
		applyDefaults(&cfg.Dest)

		switch cfg.Logging.Format {
		case "json":
			logOutput = os.Stdout
		default:
			logOutput = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		}
		logger = zerolog.New(logOutput).With().Timestamp().Logger()

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

	// Connection URI flags (preferred).
	f.StringVar(&sourceURI, "source-uri", "", `Source connection URI (e.g. "postgres://user:pass@host:5432/dbname")`)
	f.StringVar(&destURI, "dest-uri", "", `Destination connection URI (e.g. "postgres://user:pass@host:5432/dbname")`)

	// Source database flags (override URI components).
	f.StringVar(&cfg.Source.Host, "source-host", "", "Source PostgreSQL host")
	f.Uint16Var(&cfg.Source.Port, "source-port", 0, "Source PostgreSQL port")
	f.StringVar(&cfg.Source.User, "source-user", "", "Source PostgreSQL user")
	f.StringVar(&cfg.Source.Password, "source-password", "", "Source PostgreSQL password")
	f.StringVar(&cfg.Source.DBName, "source-dbname", "", "Source database name")

	// Destination database flags (override URI components).
	f.StringVar(&cfg.Dest.Host, "dest-host", "", "Destination PostgreSQL host")
	f.Uint16Var(&cfg.Dest.Port, "dest-port", 0, "Destination PostgreSQL port")
	f.StringVar(&cfg.Dest.User, "dest-user", "", "Destination PostgreSQL user")
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

func copyExplicitFlags(cmd *cobra.Command, prefix string, src, dst *config.DatabaseConfig) {
	if cmd.Flags().Changed(prefix + "-host") {
		dst.Host = src.Host
	}
	if cmd.Flags().Changed(prefix + "-port") {
		dst.Port = src.Port
	}
	if cmd.Flags().Changed(prefix + "-user") {
		dst.User = src.User
	}
	if cmd.Flags().Changed(prefix + "-password") {
		dst.Password = src.Password
	}
	if cmd.Flags().Changed(prefix + "-dbname") {
		dst.DBName = src.DBName
	}
}

func applyExplicitFlags(cmd *cobra.Command, prefix string, dst *config.DatabaseConfig) {
	if cmd.Flags().Changed(prefix + "-host") {
		v, _ := cmd.Flags().GetString(prefix + "-host")
		dst.Host = v
	}
	if cmd.Flags().Changed(prefix + "-port") {
		v, _ := cmd.Flags().GetUint16(prefix + "-port")
		dst.Port = v
	}
	if cmd.Flags().Changed(prefix + "-user") {
		v, _ := cmd.Flags().GetString(prefix + "-user")
		dst.User = v
	}
	if cmd.Flags().Changed(prefix + "-password") {
		v, _ := cmd.Flags().GetString(prefix + "-password")
		dst.Password = v
	}
	if cmd.Flags().Changed(prefix + "-dbname") {
		v, _ := cmd.Flags().GetString(prefix + "-dbname")
		dst.DBName = v
	}
}

func applyDefaults(d *config.DatabaseConfig) {
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
