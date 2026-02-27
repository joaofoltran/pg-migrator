export interface TableProgress {
  schema: string;
  name: string;
  status: "pending" | "copying" | "copied" | "streaming";
  rows_total: number;
  rows_copied: number;
  size_bytes: number;
  bytes_copied: number;
  percent: number;
  elapsed_sec: number;
}

export interface Snapshot {
  timestamp: string;
  phase: string;
  elapsed_sec: number;

  applied_lsn: string;
  confirmed_lsn: string;
  lag_bytes: number;
  lag_formatted: string;

  tables_total: number;
  tables_copied: number;
  tables: TableProgress[];

  rows_per_sec: number;
  bytes_per_sec: number;
  total_rows: number;
  total_bytes: number;

  error_count: number;
  last_error?: string;
}

export interface LogEntry {
  time: string;
  level: string;
  message: string;
  fields?: Record<string, string>;
}
