export type NodeRole = "primary" | "replica" | "standby";

export interface ClusterNode {
  id: string;
  name: string;
  host: string;
  port: number;
  role: NodeRole;
  agent_url?: string;
}

export interface Cluster {
  id: string;
  name: string;
  nodes: ClusterNode[];
  tags?: string[];
  created_at: string;
  updated_at: string;
}

export interface ConnTestResult {
  reachable: boolean;
  version?: string;
  is_replica: boolean;
  privileges?: Record<string, boolean>;
  latency_ns: number;
  error?: string;
}
