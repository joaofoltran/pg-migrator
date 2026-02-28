package cluster

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
)

type ClusterInfo struct {
	Version      string          `json:"version"`
	IsReplica    bool            `json:"is_replica"`
	Uptime       string          `json:"uptime"`
	StartedAt    string          `json:"started_at"`
	MaxConns     int             `json:"max_connections"`
	ClusterSize  string          `json:"cluster_size"`
	ClusterBytes int64           `json:"cluster_bytes"`
	Databases    []DBInfo        `json:"databases"`
	Parameters   []ParameterInfo `json:"parameters"`
}

type DBInfo struct {
	Name       string       `json:"name"`
	Size       string       `json:"size"`
	SizeBytes  int64        `json:"size_bytes"`
	Owner      string       `json:"owner"`
	Schemas    []SchemaInfo `json:"schemas,omitempty"`
}

type SchemaInfo struct {
	Name       string      `json:"name"`
	Tables     []TableInfo `json:"tables"`
	TableCount int         `json:"table_count"`
	TotalSize  string      `json:"total_size"`
	TotalBytes int64       `json:"total_bytes"`
}

type TableInfo struct {
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	RowCount   int64  `json:"row_count"`
	TotalSize  string `json:"total_size"`
	TotalBytes int64  `json:"total_bytes"`
	DataSize   string `json:"data_size"`
	DataBytes  int64  `json:"data_bytes"`
	IndexSize  string `json:"index_size"`
	IndexBytes int64  `json:"index_bytes"`
}

type ParameterInfo struct {
	Name    string `json:"name"`
	Setting string `json:"setting"`
	Unit    string `json:"unit,omitempty"`
	Source  string `json:"source"`
}

func Introspect(ctx context.Context, dsn string) (ClusterInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return ClusterInfo{}, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	var info ClusterInfo

	conn.QueryRow(ctx, "SELECT version()").Scan(&info.Version)
	conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&info.IsReplica)
	conn.QueryRow(ctx,
		"SELECT COALESCE(date_trunc('second', current_timestamp - pg_postmaster_start_time())::text, 'unknown')").Scan(&info.Uptime)
	conn.QueryRow(ctx,
		"SELECT COALESCE(to_char(pg_postmaster_start_time(), 'YYYY-MM-DD HH24:MI:SS TZ'), 'unknown')").Scan(&info.StartedAt)
	conn.QueryRow(ctx,
		"SELECT setting::int FROM pg_settings WHERE name = 'max_connections'").Scan(&info.MaxConns)

	info.Databases, _ = queryDatabases(ctx, conn)

	var totalBytes int64
	for _, db := range info.Databases {
		totalBytes += db.SizeBytes
	}
	info.ClusterBytes = totalBytes
	info.ClusterSize = formatBytes(totalBytes)

	info.Parameters, _ = queryParameters(ctx, conn)

	for i, db := range info.Databases {
		if db.Name == "template0" || db.Name == "template1" {
			continue
		}
		dbDSN := replaceDSNDatabase(dsn, db.Name)
		schemas, err := introspectDatabase(ctx, dbDSN)
		if err != nil {
			continue
		}
		info.Databases[i].Schemas = schemas
	}

	conn.Close(ctx)

	return info, nil
}

func queryDatabases(ctx context.Context, conn *pgx.Conn) ([]DBInfo, error) {
	rows, err := conn.Query(ctx, `
		SELECT d.datname, pg_database_size(d.oid), pg_size_pretty(pg_database_size(d.oid)),
		       COALESCE(r.rolname, 'unknown')
		FROM pg_database d
		LEFT JOIN pg_roles r ON r.oid = d.datdba
		WHERE d.datistemplate = false
		ORDER BY pg_database_size(d.oid) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []DBInfo
	for rows.Next() {
		var db DBInfo
		if err := rows.Scan(&db.Name, &db.SizeBytes, &db.Size, &db.Owner); err != nil {
			continue
		}
		dbs = append(dbs, db)
	}
	return dbs, rows.Err()
}

func queryParameters(ctx context.Context, conn *pgx.Conn) ([]ParameterInfo, error) {
	rows, err := conn.Query(ctx, `
		SELECT name, setting, COALESCE(unit, ''), source
		FROM pg_settings
		WHERE source != 'default'
		  AND source != 'client'
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var params []ParameterInfo
	for rows.Next() {
		var p ParameterInfo
		if err := rows.Scan(&p.Name, &p.Setting, &p.Unit, &p.Source); err != nil {
			continue
		}
		params = append(params, p)
	}
	return params, rows.Err()
}

func introspectDatabase(ctx context.Context, dsn string) ([]SchemaInfo, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	return querySchemas(ctx, conn)
}

func querySchemas(ctx context.Context, conn *pgx.Conn) ([]SchemaInfo, error) {
	rows, err := conn.Query(ctx, `
		SELECT
			n.nspname AS schema_name,
			c.relname AS table_name,
			COALESCE(s.n_live_tup, 0) AS row_count,
			pg_total_relation_size(c.oid) AS total_bytes,
			pg_size_pretty(pg_total_relation_size(c.oid)) AS total_size,
			pg_relation_size(c.oid) AS data_bytes,
			pg_size_pretty(pg_relation_size(c.oid)) AS data_size,
			pg_indexes_size(c.oid) AS index_bytes,
			pg_size_pretty(pg_indexes_size(c.oid)) AS index_size
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
		WHERE c.relkind = 'r'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY n.nspname, pg_total_relation_size(c.oid) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schemaMap := make(map[string]*SchemaInfo)
	var schemaOrder []string

	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(
			&t.Schema, &t.Name, &t.RowCount,
			&t.TotalBytes, &t.TotalSize,
			&t.DataBytes, &t.DataSize,
			&t.IndexBytes, &t.IndexSize,
		); err != nil {
			continue
		}

		si, ok := schemaMap[t.Schema]
		if !ok {
			si = &SchemaInfo{Name: t.Schema}
			schemaMap[t.Schema] = si
			schemaOrder = append(schemaOrder, t.Schema)
		}
		si.Tables = append(si.Tables, t)
		si.TableCount++
		si.TotalBytes += t.TotalBytes
	}

	var schemas []SchemaInfo
	for _, name := range schemaOrder {
		si := schemaMap[name]
		si.TotalSize = formatBytes(si.TotalBytes)
		schemas = append(schemas, *si)
	}
	return schemas, rows.Err()
}

func replaceDSNDatabase(dsn, dbname string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + dbname
	return u.String()
}

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(tb))
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f kB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
