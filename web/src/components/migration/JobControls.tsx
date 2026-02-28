import { useState, useEffect } from "react";
import { Play, Square, Loader2, ChevronDown } from "lucide-react";
import { submitClone, submitFollow, stopJob, fetchClusters } from "../../api/client";
import type { Cluster } from "../../types/cluster";

interface Props {
  idle: boolean;
  connected: boolean;
}

export function JobControls({ idle, connected }: Props) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showMenu, setShowMenu] = useState(false);
  const [showForm, setShowForm] = useState<"clone" | "follow" | null>(null);

  async function handleStop() {
    setLoading(true);
    setError(null);
    try {
      await stopJob();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to stop job");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="relative flex items-center gap-3">
      {error && (
        <span className="text-xs text-red-400 max-w-[300px] truncate">{error}</span>
      )}

      {idle ? (
        <div className="relative">
          <button
            disabled={!connected || loading}
            onClick={() => setShowMenu((v) => !v)}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
            style={{
              backgroundColor: "var(--color-accent)",
              color: "#fff",
            }}
          >
            {loading ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Play className="w-4 h-4" />
            )}
            Start
            <ChevronDown className="w-3.5 h-3.5 ml-0.5" />
          </button>

          {showMenu && !showForm && (
            <>
              <div className="fixed inset-0 z-10" onClick={() => setShowMenu(false)} />
              <div
                className="absolute right-0 top-full mt-1 z-20 rounded-lg border py-1 min-w-[160px]"
                style={{
                  backgroundColor: "var(--color-surface)",
                  borderColor: "var(--color-border)",
                }}
              >
                <button
                  className="w-full text-left px-4 py-2 text-sm transition-colors hover:bg-white/5"
                  style={{ color: "var(--color-text)" }}
                  onClick={() => { setShowMenu(false); setShowForm("clone"); }}
                >
                  Clone
                  <span className="block text-xs" style={{ color: "var(--color-text-muted)" }}>
                    Full copy + streaming
                  </span>
                </button>
                <button
                  className="w-full text-left px-4 py-2 text-sm transition-colors hover:bg-white/5"
                  style={{ color: "var(--color-text)" }}
                  onClick={() => { setShowMenu(false); setShowForm("follow"); }}
                >
                  Follow
                  <span className="block text-xs" style={{ color: "var(--color-text-muted)" }}>
                    CDC streaming only
                  </span>
                </button>
              </div>
            </>
          )}
        </div>
      ) : (
        <button
          disabled={!connected || loading}
          onClick={handleStop}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
          style={{
            backgroundColor: "#dc2626",
            color: "#fff",
          }}
        >
          {loading ? (
            <Loader2 className="w-4 h-4 animate-spin" />
          ) : (
            <Square className="w-4 h-4" />
          )}
          Stop
        </button>
      )}

      {showForm && (
        <MigrationFormModal
          mode={showForm}
          onClose={() => setShowForm(null)}
          onStarted={() => { setShowForm(null); setError(null); }}
          onError={(msg) => { setShowForm(null); setError(msg); }}
        />
      )}
    </div>
  );
}

function MigrationFormModal({
  mode,
  onClose,
  onStarted,
  onError,
}: {
  mode: "clone" | "follow";
  onClose: () => void;
  onStarted: () => void;
  onError: (msg: string) => void;
}) {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [sourceCluster, setSourceCluster] = useState("");
  const [sourceNode, setSourceNode] = useState("");
  const [destCluster, setDestCluster] = useState("");
  const [destNode, setDestNode] = useState("");
  const [follow, setFollow] = useState(mode === "clone");
  const [workers, setWorkers] = useState("4");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    fetchClusters()
      .then((data) => setClusters(data || []))
      .catch(() => {});
  }, []);

  const sourceC = clusters.find((c) => c.id === sourceCluster);
  const destC = clusters.find((c) => c.id === destCluster);

  function getNodeDSN(cluster: Cluster | undefined, nodeId: string): string {
    if (!cluster) return "";
    const node = cluster.nodes.find((n) => n.id === nodeId);
    if (!node) return "";
    const user = node.user || "postgres";
    const port = node.port || 5432;
    const dbname = node.dbname || "postgres";
    if (node.password) {
      return `postgres://${user}:${node.password}@${node.host}:${port}/${dbname}`;
    }
    return `postgres://${user}@${node.host}:${port}/${dbname}`;
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);

    const sourceURI = getNodeDSN(sourceC, sourceNode);
    const destURI = getNodeDSN(destC, destNode);

    if (!sourceURI || !destURI) {
      onError("Select source and destination clusters");
      setSubmitting(false);
      return;
    }

    try {
      if (mode === "clone") {
        await submitClone({
          source_uri: sourceURI,
          dest_uri: destURI,
          follow,
          workers: parseInt(workers) || 4,
        });
      } else {
        await submitFollow({
          source_uri: sourceURI,
          dest_uri: destURI,
        });
      }
      onStarted();
    } catch (err: unknown) {
      onError(err instanceof Error ? err.message : "Failed to start");
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
    <>
      <div className="fixed inset-0 z-40 bg-black/50" onClick={onClose} />
      <div
        className="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 z-50 w-full max-w-lg rounded-xl border p-6 space-y-5"
        style={{
          backgroundColor: "var(--color-surface)",
          borderColor: "var(--color-border)",
        }}
      >
        <div>
          <h3 className="text-base font-semibold" style={{ color: "var(--color-text)" }}>
            Start {mode === "clone" ? "Clone" : "Follow"}
          </h3>
          <p className="text-xs mt-1" style={{ color: "var(--color-text-muted)" }}>
            {mode === "clone"
              ? "Copy all data from source to destination, then stream changes."
              : "Stream CDC changes from source to destination."}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <ClusterSelector
            label="Source"
            clusters={clusters}
            selectedCluster={sourceCluster}
            selectedNode={sourceNode}
            onClusterChange={(cid) => {
              setSourceCluster(cid);
              const c = clusters.find((x) => x.id === cid);
              const primary = c?.nodes.find((n) => n.role === "primary");
              setSourceNode(primary?.id || c?.nodes[0]?.id || "");
            }}
            onNodeChange={setSourceNode}
            inputStyle={inputStyle}
          />

          <ClusterSelector
            label="Destination"
            clusters={clusters}
            selectedCluster={destCluster}
            selectedNode={destNode}
            onClusterChange={(cid) => {
              setDestCluster(cid);
              const c = clusters.find((x) => x.id === cid);
              const primary = c?.nodes.find((n) => n.role === "primary");
              setDestNode(primary?.id || c?.nodes[0]?.id || "");
            }}
            onNodeChange={setDestNode}
            inputStyle={inputStyle}
          />

          {mode === "clone" && (
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs mb-1" style={{ color: "var(--color-text-secondary)" }}>
                  Parallel workers
                </label>
                <input
                  type="number"
                  min="1"
                  max="32"
                  className="w-full rounded-md border px-3 py-2 text-sm"
                  style={inputStyle}
                  value={workers}
                  onChange={(e) => setWorkers(e.target.value)}
                />
              </div>
              <div className="flex items-end pb-1">
                <label className="flex items-center gap-2 text-sm cursor-pointer" style={{ color: "var(--color-text-secondary)" }}>
                  <input
                    type="checkbox"
                    checked={follow}
                    onChange={(e) => setFollow(e.target.checked)}
                    className="rounded"
                  />
                  Continue streaming after copy
                </label>
              </div>
            </div>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-lg text-sm transition-colors"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting || !sourceCluster || !destCluster}
              className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
              style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
            >
              {submitting ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Play className="w-4 h-4" />
              )}
              Start {mode === "clone" ? "Clone" : "Follow"}
            </button>
          </div>
        </form>
      </div>
    </>
  );
}

function ClusterSelector({
  label,
  clusters,
  selectedCluster,
  selectedNode,
  onClusterChange,
  onNodeChange,
  inputStyle,
}: {
  label: string;
  clusters: Cluster[];
  selectedCluster: string;
  selectedNode: string;
  onClusterChange: (id: string) => void;
  onNodeChange: (id: string) => void;
  inputStyle: React.CSSProperties;
}) {
  const cluster = clusters.find((c) => c.id === selectedCluster);

  return (
    <div className="space-y-2">
      <label className="block text-xs font-medium" style={{ color: "var(--color-text-secondary)" }}>
        {label}
      </label>
      <div className="grid grid-cols-2 gap-2">
        <select
          className="w-full rounded-md border px-3 py-2 text-sm"
          style={inputStyle}
          value={selectedCluster}
          onChange={(e) => onClusterChange(e.target.value)}
          required
        >
          <option value="">Select cluster...</option>
          {clusters.map((c) => (
            <option key={c.id} value={c.id}>{c.name}</option>
          ))}
        </select>

        {cluster && cluster.nodes.length > 1 && (
          <select
            className="w-full rounded-md border px-3 py-2 text-sm"
            style={inputStyle}
            value={selectedNode}
            onChange={(e) => onNodeChange(e.target.value)}
          >
            {cluster.nodes.map((n) => (
              <option key={n.id} value={n.id}>
                {n.role}: {n.host}:{n.port}
              </option>
            ))}
          </select>
        )}

        {cluster && cluster.nodes.length === 1 && (
          <div className="flex items-center px-3 text-sm font-mono" style={{ color: "var(--color-text-muted)" }}>
            {cluster.nodes[0].host}:{cluster.nodes[0].port}
          </div>
        )}
      </div>
    </div>
  );
}
