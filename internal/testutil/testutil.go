package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultSourceDSN = "postgres://postgres:source@localhost:55432/source?sslmode=disable"
	DefaultDestDSN   = "postgres://postgres:dest@localhost:55433/dest?sslmode=disable"
)

func SourceDSN() string {
	if v := os.Getenv("PGMANAGER_SOURCE_DSN"); v != "" {
		return v
	}
	return DefaultSourceDSN
}

func DestDSN() string {
	if v := os.Getenv("PGMANAGER_DEST_DSN"); v != "" {
		return v
	}
	return DefaultDestDSN
}

func ContainerRuntime() string {
	if v := os.Getenv("CONTAINER_RUNTIME"); v != "" {
		return v
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}
	return ""
}

func ComposeCommand() (string, []string) {
	rt := ContainerRuntime()
	switch rt {
	case "podman":
		if _, err := exec.LookPath("podman-compose"); err == nil {
			return "podman-compose", nil
		}
		return "podman", []string{"compose"}
	default:
		return rt, []string{"compose"}
	}
}

func ProjectRoot() string {
	if v := os.Getenv("PGMANAGER_ROOT"); v != "" {
		return v
	}
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	d, _ := os.Getwd()
	return d
}

func RunCompose(args ...string) error {
	bin, baseArgs := ComposeCommand()
	if bin == "" {
		return fmt.Errorf("no container runtime found (install docker or podman)")
	}

	composeFile := os.Getenv("COMPOSE_FILE")
	if composeFile == "" {
		composeFile = "docker-compose.test.yml"
	}

	root := ProjectRoot()
	absCompose := filepath.Join(root, composeFile)

	fullArgs := append(baseArgs, "-f", absCompose)
	fullArgs = append(fullArgs, args...)
	cmd := exec.Command(bin, fullArgs...)
	cmd.Dir = root
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func StartContainers(t *testing.T) {
	t.Helper()
	rt := ContainerRuntime()
	if rt == "" {
		t.Skip("no container runtime found (docker or podman); skipping integration tests")
	}
	t.Logf("using container runtime: %s", rt)

	if err := RunCompose("up", "-d", "--wait"); err != nil {
		if strings.Contains(err.Error(), "unknown flag: --wait") {
			if err2 := RunCompose("up", "-d"); err2 != nil {
				t.Fatalf("compose up failed: %v", err2)
			}
			waitForContainerHealth(t, 60*time.Second)
		} else {
			t.Fatalf("compose up failed: %v", err)
		}
	}
}

func StopContainers(t *testing.T) {
	t.Helper()
	if err := RunCompose("down", "-v"); err != nil {
		t.Logf("compose down failed (non-fatal): %v", err)
	}
}

func waitForContainerHealth(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		srcOK := TryPing(SourceDSN())
		dstOK := TryPing(DestDSN())
		if srcOK && dstOK {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("timed out waiting for database containers to become healthy")
}

func TryPing(dsn string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return false
	}
	defer pool.Close()
	return pool.Ping(ctx) == nil
}

func MustConnectPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", dsn, err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database not reachable at %s: %v", dsn, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func CreateTestTable(t *testing.T, pool *pgxpool.Pool, schema, table string, rowCount int) {
	t.Helper()
	ctx := context.Background()

	qn := quoteQN(schema, table)

	_, err := pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", qn))
	if err != nil {
		t.Fatalf("drop table %s: %v", qn, err)
	}

	_, err = pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER NOT NULL DEFAULT 0
		)`, qn))
	if err != nil {
		t.Fatalf("create table %s: %v", qn, err)
	}

	for i := 1; i <= rowCount; i++ {
		_, err := pool.Exec(ctx, fmt.Sprintf(
			"INSERT INTO %s (name, value) VALUES ($1, $2)", qn),
			fmt.Sprintf("row-%d", i), i*10)
		if err != nil {
			t.Fatalf("insert row %d into %s: %v", i, qn, err)
		}
	}
}

func DropTestTable(t *testing.T, pool *pgxpool.Pool, schema, table string) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), fmt.Sprintf(
		"DROP TABLE IF EXISTS %s CASCADE", quoteQN(schema, table)))
}

func TableRowCount(t *testing.T, pool *pgxpool.Pool, schema, table string) int64 {
	t.Helper()
	var count int64
	err := pool.QueryRow(context.Background(), fmt.Sprintf(
		"SELECT COUNT(*) FROM %s", quoteQN(schema, table))).Scan(&count)
	if err != nil {
		t.Fatalf("count rows in %s: %v", quoteQN(schema, table), err)
	}
	return count
}

func TableExists(t *testing.T, pool *pgxpool.Pool, schema, table string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)`, schema, table).Scan(&exists)
	if err != nil {
		t.Fatalf("check table existence: %v", err)
	}
	return exists
}

func DropPublication(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), fmt.Sprintf(
		"DROP PUBLICATION IF EXISTS %s", quoteIdent(name)))
}

func CreatePublication(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", quoteIdent(name)))
	_, err := pool.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", quoteIdent(name)))
	if err != nil {
		t.Fatalf("create publication %s: %v", name, err)
	}
}

func DropReplicationSlot(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), fmt.Sprintf(
		"SELECT pg_drop_replication_slot('%s')", name))
}

func CleanupReplication(t *testing.T, pool *pgxpool.Pool, slotName, pubName string) {
	t.Helper()
	DropReplicationSlot(t, pool, slotName)
	DropPublication(t, pool, pubName)
}

func quoteIdent(s string) string {
	return `"` + s + `"`
}

func quoteQN(schema, table string) string {
	if schema == "" || schema == "public" {
		return quoteIdent(table)
	}
	return quoteIdent(schema) + "." + quoteIdent(table)
}
