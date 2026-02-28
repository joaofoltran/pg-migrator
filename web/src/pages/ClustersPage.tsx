import { useEffect, useState } from "react";
import {
  Database,
  Plus,
  Trash2,
  Server,
  CheckCircle2,
  XCircle,
  Loader2,
  ChevronRight,
} from "lucide-react";
import type { Cluster, ClusterNode } from "../types/cluster";
import { fetchClusters, addCluster, removeCluster } from "../api/client";

export function ClustersPage() {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);

  async function load() {
    try {
      const data = await fetchClusters();
      setClusters(data || []);
    } catch {
      setClusters([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function handleRemove(id: string) {
    try {
      await removeCluster(id);
      setClusters((prev) => prev.filter((c) => c.id !== id));
    } catch (e: unknown) {
      alert(e instanceof Error ? e.message : "Failed to remove cluster");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2
          className="w-8 h-8 animate-spin"
          style={{ color: "var(--color-accent)" }}
        />
      </div>
    );
  }

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center justify-between">
        <div>
          <h2
            className="text-lg font-semibold"
            style={{ color: "var(--color-text)" }}
          >
            Clusters
          </h2>
          <p
            className="text-sm mt-0.5"
            style={{ color: "var(--color-text-muted)" }}
          >
            Registered PostgreSQL clusters
          </p>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
        >
          <Plus className="w-4 h-4" />
          Add Cluster
        </button>
      </div>

      {showAdd && (
        <AddClusterForm
          onAdded={(c) => {
            setClusters((prev) => [...prev, c]);
            setShowAdd(false);
          }}
          onCancel={() => setShowAdd(false)}
        />
      )}

      {clusters.length === 0 && !showAdd ? (
        <div
          className="rounded-lg border p-12 text-center"
          style={{
            backgroundColor: "var(--color-surface)",
            borderColor: "var(--color-border)",
          }}
        >
          <Database
            className="w-12 h-12 mx-auto mb-4"
            style={{ color: "var(--color-text-muted)" }}
          />
          <h3
            className="text-sm font-medium mb-1"
            style={{ color: "var(--color-text)" }}
          >
            No clusters registered
          </h3>
          <p
            className="text-sm"
            style={{ color: "var(--color-text-muted)" }}
          >
            Add your first PostgreSQL cluster to get started.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {clusters.map((c) => (
            <div
              key={c.id}
              className="rounded-lg border"
              style={{
                backgroundColor: "var(--color-surface)",
                borderColor: "var(--color-border)",
              }}
            >
              <div
                className="flex items-center gap-3 p-4 cursor-pointer"
                onClick={() =>
                  setExpanded(expanded === c.id ? null : c.id)
                }
              >
                <ChevronRight
                  className={`w-4 h-4 transition-transform ${expanded === c.id ? "rotate-90" : ""}`}
                  style={{ color: "var(--color-text-muted)" }}
                />
                <div
                  className="w-8 h-8 rounded-lg flex items-center justify-center"
                  style={{ backgroundColor: "var(--color-accent)" }}
                >
                  <Database className="w-4 h-4 text-white" />
                </div>
                <div className="flex-1 min-w-0">
                  <div
                    className="text-sm font-medium"
                    style={{ color: "var(--color-text)" }}
                  >
                    {c.name}
                  </div>
                  <div
                    className="text-xs"
                    style={{ color: "var(--color-text-muted)" }}
                  >
                    {c.id} &middot; {c.nodes.length} node
                    {c.nodes.length !== 1 ? "s" : ""}
                  </div>
                </div>
                {c.tags && c.tags.length > 0 && (
                  <div className="flex gap-1">
                    {c.tags.map((tag) => (
                      <span
                        key={tag}
                        className="text-[10px] px-2 py-0.5 rounded-full"
                        style={{
                          backgroundColor: "var(--color-border)",
                          color: "var(--color-text-muted)",
                        }}
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    handleRemove(c.id);
                  }}
                  className="p-1.5 rounded-md hover:bg-red-500/10 transition-colors"
                >
                  <Trash2 className="w-4 h-4 text-red-400" />
                </button>
              </div>

              {expanded === c.id && (
                <div
                  className="border-t px-4 py-3"
                  style={{ borderColor: "var(--color-border)" }}
                >
                  <div className="grid gap-2">
                    {c.nodes.map((n) => (
                      <NodeRow key={n.id} node={n} />
                    ))}
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function NodeRow({ node }: { node: ClusterNode }) {
  const roleColors: Record<string, string> = {
    primary: "#22c55e",
    replica: "#3b82f6",
    standby: "#eab308",
  };
  const color = roleColors[node.role] || "var(--color-text-muted)";

  return (
    <div
      className="flex items-center gap-3 px-3 py-2 rounded-md"
      style={{ backgroundColor: "var(--color-bg)" }}
    >
      <Server className="w-4 h-4" style={{ color }} />
      <span
        className="text-xs font-mono px-1.5 py-0.5 rounded"
        style={{ backgroundColor: color + "20", color }}
      >
        {node.role}
      </span>
      <span
        className="text-sm font-mono flex-1"
        style={{ color: "var(--color-text)" }}
      >
        {node.host}:{node.port}
      </span>
      {node.agent_url && (
        <span
          className="text-[10px] px-2 py-0.5 rounded-full"
          style={{
            backgroundColor: "var(--color-accent)" + "20",
            color: "var(--color-accent)",
          }}
        >
          agent
        </span>
      )}
    </div>
  );
}

function AddClusterForm({
  onAdded,
  onCancel,
}: {
  onAdded: (c: Cluster) => void;
  onCancel: () => void;
}) {
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("5432");
  const [role, setRole] = useState<string>("primary");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const c = await addCluster({
        id,
        name,
        nodes: [
          {
            id: role,
            name: `${host}-${role}`,
            host,
            port: parseInt(port) || 5432,
            role: role as ClusterNode["role"],
          },
        ],
      });
      onAdded(c);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to add cluster");
    } finally {
      setSubmitting(false);
    }
  }

  const inputStyle = {
    backgroundColor: "var(--color-bg)",
    borderColor: "var(--color-border)",
    color: "var(--color-text)",
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-lg border p-5 space-y-4"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border)",
      }}
    >
      <h3
        className="text-sm font-medium"
        style={{ color: "var(--color-text)" }}
      >
        Register New Cluster
      </h3>

      {error && (
        <div className="flex items-center gap-2 text-xs text-red-400">
          <XCircle className="w-3.5 h-3.5" />
          {error}
        </div>
      )}

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label
            className="block text-xs mb-1"
            style={{ color: "var(--color-text-secondary)" }}
          >
            Cluster ID
          </label>
          <input
            className="w-full rounded-md border px-3 py-2 text-sm"
            style={inputStyle}
            placeholder="prod"
            value={id}
            onChange={(e) => setId(e.target.value)}
            required
          />
        </div>
        <div>
          <label
            className="block text-xs mb-1"
            style={{ color: "var(--color-text-secondary)" }}
          >
            Cluster Name
          </label>
          <input
            className="w-full rounded-md border px-3 py-2 text-sm"
            style={inputStyle}
            placeholder="Production"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
        </div>
      </div>

      <div
        className="border-t pt-3"
        style={{ borderColor: "var(--color-border)" }}
      >
        <p
          className="text-xs mb-2"
          style={{ color: "var(--color-text-muted)" }}
        >
          Primary node
        </p>
        <div className="grid grid-cols-3 gap-3">
          <div>
            <label
              className="block text-xs mb-1"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Host
            </label>
            <input
              className="w-full rounded-md border px-3 py-2 text-sm"
              style={inputStyle}
              placeholder="10.0.0.1"
              value={host}
              onChange={(e) => setHost(e.target.value)}
              required
            />
          </div>
          <div>
            <label
              className="block text-xs mb-1"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Port
            </label>
            <input
              className="w-full rounded-md border px-3 py-2 text-sm"
              style={inputStyle}
              placeholder="5432"
              value={port}
              onChange={(e) => setPort(e.target.value)}
              required
            />
          </div>
          <div>
            <label
              className="block text-xs mb-1"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Role
            </label>
            <select
              className="w-full rounded-md border px-3 py-2 text-sm"
              style={inputStyle}
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              <option value="primary">Primary</option>
              <option value="replica">Replica</option>
              <option value="standby">Standby</option>
            </select>
          </div>
        </div>
      </div>

      <div className="flex items-center justify-end gap-2 pt-2">
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-2 rounded-lg text-sm transition-colors"
          style={{ color: "var(--color-text-secondary)" }}
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={submitting}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
          style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
        >
          {submitting ? (
            <Loader2 className="w-4 h-4 animate-spin" />
          ) : (
            <CheckCircle2 className="w-4 h-4" />
          )}
          Register
        </button>
      </div>
    </form>
  );
}
