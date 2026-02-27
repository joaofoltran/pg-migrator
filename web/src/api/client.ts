import type { Snapshot, LogEntry } from "../types/metrics";

const BASE = "";

export async function fetchStatus(): Promise<Snapshot> {
  const res = await fetch(`${BASE}/api/v1/status`);
  return res.json();
}

export async function fetchTables(): Promise<Snapshot["tables"]> {
  const res = await fetch(`${BASE}/api/v1/tables`);
  return res.json();
}

export async function fetchLogs(): Promise<LogEntry[]> {
  const res = await fetch(`${BASE}/api/v1/logs`);
  return res.json();
}
