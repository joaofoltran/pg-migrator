CREATE TABLE clusters (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    tags       TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE nodes (
    id         TEXT NOT NULL,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    host       TEXT NOT NULL,
    port       INTEGER NOT NULL DEFAULT 5432,
    role       TEXT NOT NULL DEFAULT 'primary',
    username   TEXT NOT NULL DEFAULT 'postgres',
    password   TEXT NOT NULL DEFAULT '',
    dbname     TEXT NOT NULL DEFAULT 'postgres',
    agent_url  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (cluster_id, id)
);

CREATE INDEX idx_nodes_cluster ON nodes(cluster_id);
