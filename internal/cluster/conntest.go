package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type ConnTestResult struct {
	Reachable  bool              `json:"reachable"`
	Version    string            `json:"version,omitempty"`
	IsReplica  bool              `json:"is_replica"`
	Privileges map[string]bool   `json:"privileges,omitempty"`
	Latency    time.Duration     `json:"latency_ns"`
	Error      string            `json:"error,omitempty"`
}

func TestConnection(ctx context.Context, dsn string) ConnTestResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	start := time.Now()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return ConnTestResult{
			Reachable: false,
			Error:     fmt.Sprintf("connect: %s", err.Error()),
			Latency:   time.Since(start),
		}
	}
	defer conn.Close(ctx)

	latency := time.Since(start)

	result := ConnTestResult{
		Reachable:  true,
		Latency:    latency,
		Privileges: make(map[string]bool),
	}

	var version string
	if err := conn.QueryRow(ctx, "SELECT version()").Scan(&version); err == nil {
		result.Version = version
	}

	var inRecovery bool
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err == nil {
		result.IsReplica = inRecovery
	}

	result.Privileges["connect"] = true

	var canReplicate bool
	err = conn.QueryRow(ctx,
		"SELECT rolreplication FROM pg_roles WHERE rolname = current_user").Scan(&canReplicate)
	if err == nil {
		result.Privileges["replication"] = canReplicate
	}

	var hasCreateDB bool
	err = conn.QueryRow(ctx,
		"SELECT rolcreatedb FROM pg_roles WHERE rolname = current_user").Scan(&hasCreateDB)
	if err == nil {
		result.Privileges["createdb"] = hasCreateDB
	}

	var isSuperuser bool
	err = conn.QueryRow(ctx,
		"SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&isSuperuser)
	if err == nil {
		result.Privileges["superuser"] = isSuperuser
	}

	return result
}
