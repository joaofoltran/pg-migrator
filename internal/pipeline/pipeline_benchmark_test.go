//go:build benchmark

package pipeline_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmigrator/internal/config"
	"github.com/jfoltran/pgmigrator/internal/pipeline"
	"github.com/jfoltran/pgmigrator/internal/testutil"
)

const (
	targetSizeGB    = 25
	bytesPerGB      = 1024 * 1024 * 1024
	approxRowSizeB  = 512
	totalTargetRows = int64(targetSizeGB) * int64(bytesPerGB) / int64(approxRowSizeB)
	seedBatchSize   = 100_000
	seedWorkers     = 4
)

type benchTable struct {
	name string
	pct  float64
}

var benchTables = []benchTable{
	{"bench_events", 0.40},
	{"bench_logs", 0.25},
	{"bench_metrics", 0.20},
	{"bench_users", 0.10},
	{"bench_sessions", 0.05},
}

func TestMain(m *testing.M) {
	rt := testutil.ContainerRuntime()
	if rt == "" {
		fmt.Fprintln(os.Stderr, "SKIP: no container runtime found (docker or podman)")
		os.Exit(0)
	}

	alreadyRunning := testutil.TryPing(testutil.SourceDSN()) && testutil.TryPing(testutil.DestDSN())

	if !alreadyRunning {
		fmt.Fprintf(os.Stderr, "starting benchmark containers with %s...\n", rt)
		os.Setenv("COMPOSE_FILE", "docker-compose.bench.yml")
		if err := testutil.RunCompose("up", "-d", "--wait"); err != nil {
			if err2 := testutil.RunCompose("up", "-d"); err2 != nil {
				fmt.Fprintf(os.Stderr, "compose up failed: %v\n", err2)
				os.Exit(1)
			}
			if err := waitForBenchDBs(120 * time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "databases not ready: %v\n", err)
				os.Exit(1)
			}
		}
	}

	code := m.Run()

	if !alreadyRunning {
		fmt.Fprintln(os.Stderr, "stopping benchmark containers...")
		_ = testutil.RunCompose("down", "-v")
	}

	os.Exit(code)
}

func waitForBenchDBs(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if testutil.TryPing(testutil.SourceDSN()) && testutil.TryPing(testutil.DestDSN()) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s", timeout)
}

func benchConfig(slotName, pubName string) *config.Config {
	return &config.Config{
		Source: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "source",
			DBName:   "source",
		},
		Dest: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5433,
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
			Workers: 4,
		},
	}
}

func seedBenchTable(t *testing.T, pool *pgxpool.Pool, name string, rows int64) {
	t.Helper()
	ctx := context.Background()
	qn := fmt.Sprintf(`"%s"`, name)

	t.Logf("creating table %s ...", name)
	_, err := pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", qn))
	if err != nil {
		t.Fatalf("drop %s: %v", name, err)
	}

	_, err = pool.Exec(ctx, fmt.Sprintf(`
		CREATE UNLOGGED TABLE %s (
			id         BIGSERIAL PRIMARY KEY,
			ts         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			category   SMALLINT    NOT NULL,
			source_id  UUID        NOT NULL DEFAULT gen_random_uuid(),
			payload    TEXT        NOT NULL,
			score      NUMERIC(12,4) NOT NULL DEFAULT 0,
			tags       TEXT[]      NOT NULL DEFAULT '{}',
			metadata   JSONB       NOT NULL DEFAULT '{}'
		)`, qn))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}

	t.Logf("seeding %s with %s rows using %d workers ...", name, fmtCount(rows), seedWorkers)
	start := time.Now()
	var inserted atomic.Int64
	var seedErr atomic.Value

	work := make(chan int64, seedWorkers*2)
	var wg sync.WaitGroup

	for w := 0; w < seedWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.Acquire(ctx)
			if err != nil {
				seedErr.Store(err)
				return
			}
			defer conn.Release()
			_, _ = conn.Exec(ctx, "SET synchronous_commit = off")

			for batch := range work {
				if seedErr.Load() != nil {
					return
				}
				_, err := conn.Exec(ctx, fmt.Sprintf(`
					INSERT INTO %s (ts, category, source_id, payload, score, tags, metadata)
					SELECT
						NOW() - (random() * INTERVAL '365 days'),
						(random() * 100)::SMALLINT,
						gen_random_uuid(),
						REPEAT('x', 250 + (random()*100)::INT),
						(random() * 100000)::NUMERIC(12,4),
						ARRAY['tag-' || (random()*50)::INT, 'cat-' || (random()*20)::INT],
						'{"k":1}'::JSONB
					FROM generate_series(1, %d)
				`, qn, batch))
				if err != nil {
					seedErr.Store(err)
					return
				}
				inserted.Add(batch)
			}
		}()
	}

	var dispatched int64
	reportTicker := time.NewTicker(10 * time.Second)
	reportDone := make(chan struct{})

	go func() {
		defer reportTicker.Stop()
		for {
			select {
			case <-reportDone:
				return
			case <-reportTicker.C:
				done := inserted.Load()
				elapsed := time.Since(start)
				rate := float64(done) / elapsed.Seconds()
				t.Logf("  %s: %s / %s rows (%.0f rows/s, %s elapsed)",
					name, fmtCount(done), fmtCount(rows), rate, elapsed.Round(time.Second))
			}
		}
	}()

	for dispatched < rows {
		batch := int64(seedBatchSize)
		if batch > rows-dispatched {
			batch = rows - dispatched
		}
		work <- batch
		dispatched += batch
	}
	close(work)
	wg.Wait()
	close(reportDone)

	if e := seedErr.Load(); e != nil {
		t.Fatalf("seed %s failed: %v", name, e)
	}

	_, _ = pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s SET LOGGED", qn))
	_, _ = pool.Exec(ctx, "ANALYZE "+qn)

	final := inserted.Load()
	elapsed := time.Since(start)
	rate := float64(final) / elapsed.Seconds()
	t.Logf("  %s: seeding complete — %s rows in %s (%.0f rows/s)",
		name, fmtCount(final), elapsed.Round(time.Second), rate)
}

type tableStats struct {
	rows int64
	size int64
}

func tableEstimates(t *testing.T, pool *pgxpool.Pool, name string) tableStats {
	t.Helper()
	var s tableStats
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(c.reltuples, 0)::BIGINT,
		        COALESCE(pg_table_size(c.oid), 0)
		 FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relname = $1`, name).Scan(&s.rows, &s.size)
	if err != nil {
		t.Fatalf("table estimates %s: %v", name, err)
	}
	return s
}

func fmtCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func fmtBytes(b int64) string {
	switch {
	case b >= int64(bytesPerGB):
		return fmt.Sprintf("%.2f GB", float64(b)/float64(bytesPerGB))
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
}

func TestBenchmark_Clone25GB(t *testing.T) {
	srcPool := testutil.MustConnectPool(t, testutil.SourceDSN())
	dstPool := testutil.MustConnectPool(t, testutil.DestDSN())

	slotName := "bench_slot"
	pubName := "bench_pub"

	t.Cleanup(func() {
		for _, bt := range benchTables {
			testutil.DropTestTable(t, srcPool, "public", bt.name)
			testutil.DropTestTable(t, dstPool, "public", bt.name)
		}
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	t.Log("=== SEEDING SOURCE DATABASE ===")
	t.Logf("target: ~%d GB across %d tables (%s total rows)",
		targetSizeGB, len(benchTables), fmtCount(totalTargetRows))

	seedStart := time.Now()
	var seedWg sync.WaitGroup
	for _, bt := range benchTables {
		seedWg.Add(1)
		go func(bt benchTable) {
			defer seedWg.Done()
			rows := int64(float64(totalTargetRows) * bt.pct)
			seedBenchTable(t, srcPool, bt.name, rows)
		}(bt)
	}
	seedWg.Wait()
	seedDuration := time.Since(seedStart)

	t.Log("")
	t.Log("=== SOURCE DATA SUMMARY (estimates from pg_class after ANALYZE) ===")
	var totalSize int64
	var totalRows int64
	for _, bt := range benchTables {
		st := tableEstimates(t, srcPool, bt.name)
		totalSize += st.size
		totalRows += st.rows
		t.Logf("  %-20s %10s rows  %10s", bt.name, fmtCount(st.rows), fmtBytes(st.size))
	}
	t.Logf("  %-20s %10s rows  %10s", "TOTAL", fmtCount(totalRows), fmtBytes(totalSize))
	t.Logf("  seed time: %s", seedDuration.Round(time.Second))
	t.Log("")

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := benchConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	t.Log("=== RUNNING CLONE ===")
	cloneStart := time.Now()
	err := p.RunClone(ctx)
	cloneDuration := time.Since(cloneStart)

	if err != nil {
		t.Fatalf("RunClone failed after %s: %v", cloneDuration.Round(time.Second), err)
	}

	t.Log("")
	t.Log("=== CLONE RESULTS ===")
	t.Logf("  total time:       %s", cloneDuration.Round(time.Second))

	if cloneDuration.Seconds() > 0 {
		throughputMBs := float64(totalSize) / (1024 * 1024) / cloneDuration.Seconds()
		throughputRowsS := float64(totalRows) / cloneDuration.Seconds()
		t.Logf("  throughput:       %.1f MB/s", throughputMBs)
		t.Logf("  throughput:       %.0f rows/s", throughputRowsS)
	}

	t.Log("")
	t.Log("=== VERIFICATION ===")
	allMatch := true
	for _, bt := range benchTables {
		srcRows := testutil.TableRowCount(t, srcPool, "public", bt.name)
		dstRows := testutil.TableRowCount(t, dstPool, "public", bt.name)
		status := "OK"
		if srcRows != dstRows {
			status = "MISMATCH"
			allMatch = false
		}
		t.Logf("  %-20s src=%s  dst=%s  %s",
			bt.name, fmtCount(srcRows), fmtCount(dstRows), status)
	}

	if !allMatch {
		t.Fatal("row count mismatch detected")
	}

	snap := p.Metrics.Snapshot()
	t.Logf("  pipeline phase:   %s", snap.Phase)
	t.Logf("  total rows:       %s", fmtCount(snap.TotalRows))
	t.Logf("  total bytes:      %s", fmtBytes(snap.TotalBytes))
}

func TestBenchmark_CloneAndFollow25GB(t *testing.T) {
	srcPool := testutil.MustConnectPool(t, testutil.SourceDSN())
	dstPool := testutil.MustConnectPool(t, testutil.DestDSN())

	tableName := "bench_cdc_events"
	slotName := "bench_cdc_slot"
	pubName := "bench_cdc_pub"

	rows := totalTargetRows / 5

	t.Cleanup(func() {
		testutil.DropTestTable(t, srcPool, "public", tableName)
		testutil.DropTestTable(t, dstPool, "public", tableName)
		testutil.CleanupReplication(t, srcPool, slotName, pubName)
	})

	t.Log("=== SEEDING SOURCE (single table for CDC test) ===")
	seedBenchTable(t, srcPool, tableName, rows)
	st := tableEstimates(t, srcPool, tableName)
	t.Logf("  %s: %s rows, %s", tableName, fmtCount(st.rows), fmtBytes(st.size))

	testutil.CreatePublication(t, srcPool, pubName)

	cfg := benchConfig(slotName, pubName)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	p := pipeline.New(cfg, logger)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	t.Log("=== RUNNING CLONE + FOLLOW ===")
	cloneStart := time.Now()

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.RunCloneAndFollow(ctx)
	}()

	waitForPhase(t, p, "streaming", 2*time.Hour)
	copyDuration := time.Since(cloneStart)
	t.Logf("  copy phase:       %s", copyDuration.Round(time.Second))

	cdcInserts := int64(100_000)
	t.Logf("  inserting %s CDC rows on source ...", fmtCount(cdcInserts))
	cdcStart := time.Now()

	for i := int64(0); i < cdcInserts; i += 10_000 {
		batch := int64(10_000)
		if i+batch > cdcInserts {
			batch = cdcInserts - i
		}
		_, err := srcPool.Exec(ctx, fmt.Sprintf(`
			INSERT INTO "%s" (ts, category, source_id, payload, score, tags, metadata)
			SELECT
				NOW(),
				(random() * 100)::SMALLINT,
				gen_random_uuid(),
				REPEAT('X', 200),
				(random() * 100000)::NUMERIC(12,4),
				ARRAY['cdc-tag'],
				'{"cdc": true}'::JSONB
			FROM generate_series(1, %d)
		`, tableName, batch))
		if err != nil {
			t.Fatalf("CDC insert at %d: %v", i, err)
		}
	}
	t.Logf("  CDC inserts done in %s", time.Since(cdcStart).Round(time.Second))

	expectedTotal := rows + cdcInserts
	t.Logf("  waiting for dest to reach %s rows ...", fmtCount(expectedTotal))

	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		got := testutil.TableRowCount(t, dstPool, "public", tableName)
		if got >= expectedTotal {
			t.Logf("  dest caught up: %s rows", fmtCount(got))
			break
		}
		time.Sleep(2 * time.Second)
	}

	dstRows := testutil.TableRowCount(t, dstPool, "public", tableName)
	totalDuration := time.Since(cloneStart)

	t.Log("")
	t.Log("=== CDC RESULTS ===")
	t.Logf("  total time:       %s", totalDuration.Round(time.Second))
	t.Logf("  copy phase:       %s", copyDuration.Round(time.Second))
	t.Logf("  cdc latency:      %s", (totalDuration - copyDuration).Round(time.Second))
	t.Logf("  source rows:      %s", fmtCount(expectedTotal))
	t.Logf("  dest rows:        %s", fmtCount(dstRows))

	if dstRows < expectedTotal {
		t.Errorf("dest missing rows: expected %s, got %s", fmtCount(expectedTotal), fmtCount(dstRows))
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Logf("RunCloneAndFollow returned: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("RunCloneAndFollow did not exit after cancel")
	}
}

func waitForPhase(t *testing.T, p *pipeline.Pipeline, target string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.Status().Phase == target {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for phase %q (current: %q)", target, p.Status().Phase)
}
