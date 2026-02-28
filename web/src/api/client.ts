import type { Snapshot, LogEntry } from "../types/metrics";
import type { Cluster, ConnTestResult } from "../types/cluster";

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

export async function submitClone(): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/jobs/clone`, { method: "POST" });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function submitFollow(): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/jobs/follow`, { method: "POST" });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function stopJob(): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/jobs/stop`, { method: "POST" });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function fetchClusters(): Promise<Cluster[]> {
  const res = await fetch(`${BASE}/api/v1/clusters`);
  return res.json();
}

export async function fetchCluster(id: string): Promise<Cluster> {
  const res = await fetch(`${BASE}/api/v1/clusters/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function addCluster(cluster: {
  id: string;
  name: string;
  nodes: Cluster["nodes"];
  tags?: string[];
}): Promise<Cluster> {
  const res = await fetch(`${BASE}/api/v1/clusters`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cluster),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function updateCluster(
  id: string,
  data: { name: string; nodes: Cluster["nodes"]; tags?: string[] }
): Promise<Cluster> {
  const res = await fetch(`${BASE}/api/v1/clusters/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function removeCluster(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/clusters/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function testConnection(dsn: string): Promise<ConnTestResult> {
  const res = await fetch(`${BASE}/api/v1/clusters/test-connection`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dsn }),
  });
  return res.json();
}
