# Cluster Management

**Package:** `internal/cluster`
**Files:** `store.go`, `conntest.go`, `store_test.go`

## Overview

The cluster package provides multi-cluster management for pgmanager. It handles registration, storage, and validation of PostgreSQL clusters and their nodes, plus connectivity testing with privilege-level detection.

Clusters are stored in a JSON file at `~/.pgmanager/clusters.json` with `0600` permissions (security-first: only the owning user can read the file).

## Data Model

### Cluster

```go
type Cluster struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Nodes     []Node    `json:"nodes"`
    Tags      []string  `json:"tags,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

### Node

```go
type Node struct {
    ID       string   `json:"id"`
    Name     string   `json:"name"`
    Host     string   `json:"host"`
    Port     uint16   `json:"port"`
    Role     NodeRole `json:"role"`
    AgentURL string   `json:"agent_url,omitempty"`
}
```

### Node Roles

| Role | Description |
|------|-------------|
| `primary` | Read-write primary node |
| `replica` | Read-only replica |
| `standby` | Standby for failover |

The `AgentURL` field is reserved for the future optional agent binary. When set, it indicates the node has a local agent available for filesystem-level operations (WAL archiving, pg_basebackup, etc.).

## Store (`store.go`)

Thread-safe JSON file store with atomic writes (write to `.tmp`, then rename).

### Construction

```go
store, err := cluster.NewStore()
```

Creates or opens `~/.pgmanager/clusters.json`. Returns an error if the file exists but can't be parsed (not if it doesn't exist — that's a fresh store).

### Operations

| Method | Description |
|--------|-------------|
| `List()` | Returns a copy of all clusters |
| `Get(id)` | Returns a cluster by ID |
| `Add(c)` | Add a new cluster (sets `created_at`, `updated_at`) |
| `Update(c)` | Update an existing cluster (preserves `created_at`) |
| `Remove(id)` | Remove a cluster by ID |
| `AddNode(clusterID, node)` | Add a node to an existing cluster |
| `RemoveNode(clusterID, nodeID)` | Remove a node from a cluster |

All mutating operations write atomically:

```go
func (s *Store) save() error {
    data, _ := json.MarshalIndent(s.data, "", "  ")
    tmp := s.path + ".tmp"
    os.WriteFile(tmp, data, 0o600)
    return os.Rename(tmp, s.path)
}
```

### Validation

```go
err := cluster.ValidateCluster(c)
```

Checks:
- Cluster ID is non-empty
- Cluster name is non-empty
- At least one node exists
- Each node has a non-empty ID, host, and non-zero port

Returns all validation errors joined via `errors.Join()`.

## Connection Testing (`conntest.go`)

```go
result := cluster.TestConnection(ctx, dsn)
```

Tests connectivity to a PostgreSQL instance and returns:

| Field | Type | Description |
|-------|------|-------------|
| `Reachable` | `bool` | Whether the connection succeeded |
| `Version` | `string` | PostgreSQL version string |
| `IsReplica` | `bool` | Result of `pg_is_in_recovery()` |
| `Privileges` | `map[string]bool` | Detected privilege levels |
| `Latency` | `time.Duration` | Connection establishment time |
| `Error` | `string` | Error message if not reachable |

### Privilege Detection

The connection test checks these privilege levels (least-privilege principle — we report what's available, never require more than needed):

| Privilege | Query | Module Usage |
|-----------|-------|-------------|
| `connect` | Implicit (connection succeeded) | All modules |
| `replication` | `SELECT rolreplication FROM pg_roles WHERE rolname = current_user` | Migration (CDC streaming) |
| `createdb` | `SELECT rolcreatedb FROM pg_roles WHERE rolname = current_user` | Schema management |
| `superuser` | `SELECT rolsuper FROM pg_roles WHERE rolname = current_user` | Reported but never required |

The test uses a 10-second timeout and creates a single connection via `pgx.Connect()`.

## API Integration

The cluster store is wired into the HTTP server via:

```go
srv.SetClusterStore(clusters)
```

This registers the following routes:

| Method | Endpoint | Handler |
|--------|----------|---------|
| `GET` | `/api/v1/clusters` | `list` |
| `POST` | `/api/v1/clusters` | `add` |
| `GET` | `/api/v1/clusters/{id}` | `get` |
| `PUT` | `/api/v1/clusters/{id}` | `update` |
| `DELETE` | `/api/v1/clusters/{id}` | `remove` |
| `POST` | `/api/v1/clusters/test-connection` | `testConnection` |

Uses Go 1.22+ path parameters (`{id}`) via `r.PathValue("id")`.

## CLI Integration

The `pgmanager cluster` command group provides:

```bash
pgmanager cluster add --id prod --name "Production" --node primary:10.0.0.1:5432
pgmanager cluster list [--json]
pgmanager cluster show prod [--json]
pgmanager cluster remove prod
pgmanager cluster test prod --user postgres --password secret --dbname postgres
```

Node specs use the format `role:host:port` where role is `primary`, `replica`, or `standby`.

## Testing

12 unit tests + 6 validation subtests covering:

- CRUD operations (add, get, list, update, remove)
- Duplicate detection (cluster IDs, node IDs)
- Node management (add/remove nodes within clusters)
- Persistence (write, reload, verify)
- File permissions (0600 enforced)
- Validation (missing fields, edge cases)

2 HTTP handler tests covering:
- Full CRUD lifecycle via API
- Validation error responses

```bash
go test ./internal/cluster/ -v
go test ./internal/server/ -run TestCluster -v
```
