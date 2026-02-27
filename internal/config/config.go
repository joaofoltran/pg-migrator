package config

import (
	"errors"
	"fmt"
	"net/url"
)

// DatabaseConfig holds connection parameters for a PostgreSQL instance.
type DatabaseConfig struct {
	Host     string
	Port     uint16
	User     string
	Password string
	DBName   string
}

// DSN returns a standard PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:   d.DBName,
	}
	return u.String()
}

// ReplicationDSN returns a connection string with replication=database set.
func (d DatabaseConfig) ReplicationDSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:     d.DBName,
		RawQuery: "replication=database",
	}
	return u.String()
}

// ReplicationConfig holds settings for the WAL replication stream.
type ReplicationConfig struct {
	SlotName     string
	Publication  string
	OutputPlugin string
	OriginID     string
}

// SnapshotConfig holds settings for the initial data copy.
type SnapshotConfig struct {
	Workers int
}

// LoggingConfig holds settings for structured logging.
type LoggingConfig struct {
	Level  string
	Format string // "json" or "console"
}

// Config is the top-level configuration for pgmigrator.
type Config struct {
	Source      DatabaseConfig
	Dest        DatabaseConfig
	Replication ReplicationConfig
	Snapshot    SnapshotConfig
	Logging     LoggingConfig
}

// Validate checks that required fields are present and values are sane.
func (c *Config) Validate() error {
	var errs []error

	if c.Source.Host == "" {
		errs = append(errs, errors.New("source host is required"))
	}
	if c.Source.DBName == "" {
		errs = append(errs, errors.New("source database name is required"))
	}
	if c.Dest.Host == "" {
		errs = append(errs, errors.New("destination host is required"))
	}
	if c.Dest.DBName == "" {
		errs = append(errs, errors.New("destination database name is required"))
	}
	if c.Replication.SlotName == "" {
		errs = append(errs, errors.New("replication slot name is required"))
	}
	if c.Replication.Publication == "" {
		errs = append(errs, errors.New("publication name is required"))
	}
	if c.Replication.OutputPlugin == "" {
		c.Replication.OutputPlugin = "pgoutput"
	}
	if c.Snapshot.Workers < 1 {
		c.Snapshot.Workers = 4
	}

	return errors.Join(errs...)
}
