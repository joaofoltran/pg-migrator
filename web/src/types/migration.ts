import type { TableProgress } from "./metrics";

export type MigrationMode = "clone_only" | "clone_and_follow" | "clone_follow_switchover";

export type MigrationStatus =
  | "created"
  | "running"
  | "streaming"
  | "switchover"
  | "completed"
  | "failed"
  | "stopped";

export interface Migration {
  id: string;
  name: string;
  source_cluster_id: string;
  dest_cluster_id: string;
  source_node_id: string;
  dest_node_id: string;
  mode: MigrationMode;
  fallback: boolean;
  status: MigrationStatus;
  phase: string;
  error_message?: string;
  slot_name: string;
  publication: string;
  copy_workers: number;
  confirmed_lsn?: string;
  tables_total: number;
  tables_copied: number;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
  live_phase?: string;
  live_lsn?: string;
  live_tables_total?: number;
  live_tables_copied?: number;
  live_tables?: TableProgress[];
  live_rows_per_sec?: number;
  live_bytes_per_sec?: number;
  live_total_rows?: number;
  live_total_bytes?: number;
}

export interface CreateMigrationRequest {
  id: string;
  name: string;
  source_cluster_id: string;
  dest_cluster_id: string;
  source_node_id: string;
  dest_node_id: string;
  mode: MigrationMode;
  fallback: boolean;
  slot_name?: string;
  publication?: string;
  copy_workers?: number;
}
