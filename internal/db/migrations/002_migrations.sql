CREATE TABLE migrations (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    source_cluster_id TEXT NOT NULL REFERENCES clusters(id),
    dest_cluster_id   TEXT NOT NULL REFERENCES clusters(id),
    source_node_id    TEXT NOT NULL,
    dest_node_id      TEXT NOT NULL,

    mode          TEXT NOT NULL DEFAULT 'clone_and_follow',
    fallback      BOOLEAN NOT NULL DEFAULT false,

    status        TEXT NOT NULL DEFAULT 'created',
    phase         TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',

    slot_name     TEXT NOT NULL DEFAULT 'pgmanager',
    publication   TEXT NOT NULL DEFAULT 'pgmanager_pub',
    copy_workers  INTEGER NOT NULL DEFAULT 4,

    confirmed_lsn TEXT NOT NULL DEFAULT '',
    tables_total  INTEGER NOT NULL DEFAULT 0,
    tables_copied INTEGER NOT NULL DEFAULT 0,

    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_migrations_status ON migrations(status);
