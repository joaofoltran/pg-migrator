//go:build integration

package pipeline_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/config"
	"github.com/jfoltran/pgmanager/internal/migration/pipeline"
	"github.com/jfoltran/pgmanager/internal/testutil"
)

func TestMain(m *testing.M) {
	rt := testutil.ContainerRuntime()
	if rt == "" {
		fmt.Fprintln(os.Stderr, "SKIP: no container runtime found (docker or podman)")
		os.Exit(0)
	}

	alreadyRunning := testutil.TryPing(testutil.SourceDSN()) && testutil.TryPing(testutil.DestDSN())

	if !alreadyRunning {
		fmt.Fprintf(os.Stderr, "starting test containers with %s...\n", rt)
		if err := testutil.RunCompose("up", "-d", "--wait"); err != nil {
			if err2 := testutil.RunCompose("up", "-d"); err2 != nil {
				fmt.Fprintf(os.Stderr, "compose up failed: %v\n", err2)
				os.Exit(1)
			}
			if err := waitForDBs(60 * time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "databases not ready: %v\n", err)
				os.Exit(1)
			}
		}
	}

	code := m.Run()

	if !alreadyRunning {
		fmt.Fprintln(os.Stderr, "stopping test containers...")
		_ = testutil.RunCompose("down", "-v")
	}

	os.Exit(code)
}

func waitForDBs(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if testutil.TryPing(testutil.SourceDSN()) && testutil.TryPing(testutil.DestDSN()) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s", timeout)
}

func testConfig(slotName, pubName string) *config.Config {
	return &config.Config{
		Source: config.DatabaseConfig{
			Host:     "localhost",
			Port:     55432,
			User:     "postgres",
			Password: "source",
			DBName:   "source",
		},
		Dest: config.DatabaseConfig{
			Host:     "localhost",
			Port:     55433,
			User:     "postgres",
			Password: "dest",
			DBName:   "dest",
		},
		Replication: config.ReplicationConfig{
			SlotName:     slotName,
			Publication:  pubName,
			OutputPlugin: "pgoutput",
		},
		Snapshot: config.SnapshotConfig{
			Workers: 2,
		},
	}
}

func setupSourceAndDest(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	srcPool := testutil.MustConnectPool(t, testutil.SourceDSN())
	dstPool := testutil.MustConnectPool(t, testutil.DestDSN())
	return srcPool, dstPool
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()%1_000_000)
}

func TestClone_SingleTable(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_clone")
	slotName := uniqueName("slot_clone")
	pubName := uniqueName("pub_clone")

	testutil.CreateTestTable(t, srcPool, "public", tableName, 100)
	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := p.RunClone(ctx)
	if err != nil {
		t.Fatalf("RunClone failed: %v", err)
	}

	if !testutil.TableExists(t, dstPool, "public", tableName) {
		t.Fatal("table was not created on destination")
	}

	got := testutil.TableRowCount(t, dstPool, "public", tableName)
	if got != 100 {
		t.Errorf("expected 100 rows on dest, got %d", got)
	}

	status := p.Status()
	if status.Phase != "done" {
		t.Errorf("expected phase 'done', got %q", status.Phase)
	}
}

func TestClone_MultipleTables(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tables := []struct {
		name string
		rows int
	}{
		{uniqueName("multi_a"), 50},
		{uniqueName("multi_b"), 200},
		{uniqueName("multi_c"), 10},
	}

	slotName := uniqueName("slot_multi")
	pubName := uniqueName("pub_multi")

	for _, tbl := range tables {
		testutil.CreateTestTable(t, srcPool, "public", tbl.name, tbl.rows)
	}
	t.Cleanup(func() {
		for _, tbl := range tables {
			testutil.DropTestTable(t, srcPool, "public", tbl.name)
			testutil.DropTestTable(t, dstPool, "public", tbl.name)
		}
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := p.RunClone(ctx); err != nil {
		t.Fatalf("RunClone failed: %v", err)
	}

	for _, tbl := range tables {
		if !testutil.TableExists(t, dstPool, "public", tbl.name) {
			t.Errorf("table %s missing on destination", tbl.name)
			continue
		}
		got := testutil.TableRowCount(t, dstPool, "public", tbl.name)
		if got != int64(tbl.rows) {
			t.Errorf("table %s: expected %d rows, got %d", tbl.name, tbl.rows, got)
		}
	}
}

func TestClone_EmptyTable(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_empty")
	slotName := uniqueName("slot_empty")
	pubName := uniqueName("pub_empty")

	testutil.CreateTestTable(t, srcPool, "public", tableName, 0)
	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := p.RunClone(ctx); err != nil {
		t.Fatalf("RunClone failed: %v", err)
	}

	if !testutil.TableExists(t, dstPool, "public", tableName) {
		t.Fatal("empty table was not created on destination")
	}

	got := testutil.TableRowCount(t, dstPool, "public", tableName)
	if got != 0 {
		t.Errorf("expected 0 rows on dest, got %d", got)
	}
}

func TestCloneAndFollow_CDCStreaming(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_cdc")
	slotName := uniqueName("slot_cdc")
	pubName := uniqueName("pub_cdc")

	testutil.CreateTestTable(t, srcPool, "public", tableName, 50)
	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.RunCloneAndFollow(ctx)
	}()

	waitForPhase(t, p, "streaming", 60*time.Second)

	for i := 1; i <= 20; i++ {
		_, err := srcPool.Exec(ctx, fmt.Sprintf(
			"INSERT INTO %s (name, value) VALUES ($1, $2)",
			quoteQN("public", tableName)),
			fmt.Sprintf("cdc-row-%d", i), i*100)
		if err != nil {
			t.Fatalf("insert cdc row %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := testutil.TableRowCount(t, dstPool, "public", tableName)
		if got >= 70 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	got := testutil.TableRowCount(t, dstPool, "public", tableName)
	if got < 70 {
		t.Errorf("expected at least 70 rows on dest (50 initial + 20 CDC), got %d", got)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Logf("RunCloneAndFollow returned: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunCloneAndFollow did not exit after cancel")
	}
}

func TestCloneAndFollow_Updates(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_upd")
	slotName := uniqueName("slot_upd")
	pubName := uniqueName("pub_upd")

	testutil.CreateTestTable(t, srcPool, "public", tableName, 10)

	_, err := srcPool.Exec(context.Background(), fmt.Sprintf(
		"ALTER TABLE %s REPLICA IDENTITY FULL", quoteQN("public", tableName)))
	if err != nil {
		t.Fatalf("set replica identity: %v", err)
	}

	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.RunCloneAndFollow(ctx)
	}()

	waitForPhase(t, p, "streaming", 60*time.Second)

	_, err = srcPool.Exec(ctx, fmt.Sprintf(
		"UPDATE %s SET value = 9999 WHERE name = 'row-1'",
		quoteQN("public", tableName)))
	if err != nil {
		t.Fatalf("update row: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var val int
		err := dstPool.QueryRow(ctx, fmt.Sprintf(
			"SELECT value FROM %s WHERE name = 'row-1'",
			quoteQN("public", tableName))).Scan(&val)
		if err == nil && val == 9999 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	var val int
	err = dstPool.QueryRow(ctx, fmt.Sprintf(
		"SELECT value FROM %s WHERE name = 'row-1'",
		quoteQN("public", tableName))).Scan(&val)
	if err != nil {
		t.Fatalf("query updated row: %v", err)
	}
	if val != 9999 {
		t.Errorf("expected updated value 9999, got %d", val)
	}

	cancel()
	<-errCh
}

func TestCloneAndFollow_Deletes(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_del")
	slotName := uniqueName("slot_del")
	pubName := uniqueName("pub_del")

	testutil.CreateTestTable(t, srcPool, "public", tableName, 20)

	_, err := srcPool.Exec(context.Background(), fmt.Sprintf(
		"ALTER TABLE %s REPLICA IDENTITY FULL", quoteQN("public", tableName)))
	if err != nil {
		t.Fatalf("set replica identity: %v", err)
	}

	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.RunCloneAndFollow(ctx)
	}()

	waitForPhase(t, p, "streaming", 60*time.Second)

	_, err = srcPool.Exec(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE name IN ('row-1', 'row-2', 'row-3')",
		quoteQN("public", tableName)))
	if err != nil {
		t.Fatalf("delete rows: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := testutil.TableRowCount(t, dstPool, "public", tableName)
		if got == 17 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	got := testutil.TableRowCount(t, dstPool, "public", tableName)
	if got != 17 {
		t.Errorf("expected 17 rows after delete, got %d", got)
	}

	cancel()
	<-errCh
}

func TestClone_SchemaOnly(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_schema")
	slotName := uniqueName("slot_schema")
	pubName := uniqueName("pub_schema")

	ctx := context.Background()
	qn := quoteQN("public", tableName)

	_, err := srcPool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			score NUMERIC(10,2) DEFAULT 0.00,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			tags TEXT[] DEFAULT '{}'
		)`, qn))
	if err != nil {
		t.Fatalf("create table with complex schema: %v", err)
	}

	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := p.RunClone(tctx); err != nil {
		t.Fatalf("RunClone failed: %v", err)
	}

	if !testutil.TableExists(t, dstPool, "public", tableName) {
		t.Fatal("table with complex schema not created on dest")
	}

	var colCount int
	err = dstPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns 
		 WHERE table_schema = 'public' AND table_name = $1`, tableName).Scan(&colCount)
	if err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if colCount != 5 {
		t.Errorf("expected 5 columns on dest, got %d", colCount)
	}
}

func TestClone_LargeDataSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large data test in short mode")
	}

	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_large")
	slotName := uniqueName("slot_large")
	pubName := uniqueName("pub_large")

	ctx := context.Background()
	qn := quoteQN("public", tableName)

	_, err := srcPool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INTEGER NOT NULL DEFAULT 0
		)`, qn))
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	_, err = srcPool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (name, value)
		SELECT 'row-' || i, i * 10
		FROM generate_series(1, 10000) AS i`, qn))
	if err != nil {
		t.Fatalf("bulk insert: %v", err)
	}

	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	tctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	if err := p.RunClone(tctx); err != nil {
		t.Fatalf("RunClone failed: %v", err)
	}

	got := testutil.TableRowCount(t, dstPool, "public", tableName)
	if got != 10000 {
		t.Errorf("expected 10000 rows, got %d", got)
	}
}

func TestClone_DataIntegrity(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_integrity")
	slotName := uniqueName("slot_integrity")
	pubName := uniqueName("pub_integrity")

	testutil.CreateTestTable(t, srcPool, "public", tableName, 100)
	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := p.RunClone(ctx); err != nil {
		t.Fatalf("RunClone failed: %v", err)
	}

	var srcSum, dstSum int64
	err := srcPool.QueryRow(ctx, fmt.Sprintf(
		"SELECT COALESCE(SUM(value), 0) FROM %s", quoteQN("public", tableName))).Scan(&srcSum)
	if err != nil {
		t.Fatalf("sum source: %v", err)
	}

	err = dstPool.QueryRow(ctx, fmt.Sprintf(
		"SELECT COALESCE(SUM(value), 0) FROM %s", quoteQN("public", tableName))).Scan(&dstSum)
	if err != nil {
		t.Fatalf("sum dest: %v", err)
	}

	if srcSum != dstSum {
		t.Errorf("data integrity mismatch: source sum=%d, dest sum=%d", srcSum, dstSum)
	}

	var srcNames, dstNames string
	_ = srcPool.QueryRow(ctx, fmt.Sprintf(
		"SELECT STRING_AGG(name, ',' ORDER BY id) FROM %s", quoteQN("public", tableName))).Scan(&srcNames)
	_ = dstPool.QueryRow(ctx, fmt.Sprintf(
		"SELECT STRING_AGG(name, ',' ORDER BY id) FROM %s", quoteQN("public", tableName))).Scan(&dstNames)

	if srcNames != dstNames {
		t.Error("row-by-row name comparison mismatch between source and dest")
	}
}

func TestPipeline_MetricsTracking(t *testing.T) {
	srcPool, dstPool := setupSourceAndDest(t)

	tableName := uniqueName("test_metrics")
	slotName := uniqueName("slot_metrics")
	pubName := uniqueName("pub_metrics")

	testutil.CreateTestTable(t, srcPool, "public", tableName, 25)
	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := testConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := p.RunClone(ctx); err != nil {
		t.Fatalf("RunClone failed: %v", err)
	}

	snap := p.Metrics.Snapshot()
	if snap.Phase != "done" {
		t.Errorf("expected metrics phase 'done', got %q", snap.Phase)
	}
	if snap.TotalRows == 0 {
		t.Error("expected non-zero TotalRows in metrics")
	}
}

func TestClone_InvalidConfig(t *testing.T) {
	cfg := &config.Config{
		Source: config.DatabaseConfig{
			Host:     "localhost",
			Port:     55432,
			User:     "postgres",
			Password: "wrong_password",
			DBName:   "nonexistent_db",
		},
		Dest: config.DatabaseConfig{
			Host:     "localhost",
			Port:     55433,
			User:     "postgres",
			Password: "dest",
			DBName:   "dest",
		},
		Replication: config.ReplicationConfig{
			SlotName:     "test_invalid",
			Publication:  "test_invalid",
			OutputPlugin: "pgoutput",
		},
		Snapshot: config.SnapshotConfig{Workers: 2},
	}

	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := p.RunClone(ctx)
	if err == nil {
		t.Fatal("expected error with invalid config, got nil")
	}
}

func waitForPhase(t *testing.T, p *pipeline.Pipeline, target string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.Status().Phase == target {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for phase %q (current: %q)", target, p.Status().Phase)
}

func quoteQN(schema, table string) string {
	if schema == "" || schema == "public" {
		return `"` + table + `"`
	}
	return `"` + schema + `"."` + table + `"`
}
