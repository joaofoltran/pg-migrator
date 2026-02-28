import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  ArrowLeft,
  ArrowRight,
  Loader2,
  CheckCircle2,
  XCircle,
  Database,
  ChevronRight,
} from "lucide-react";
import type { Cluster, ClusterNode } from "../types/cluster";
import type { MigrationMode, CreateMigrationRequest } from "../types/migration";
import { fetchClusters, createMigration } from "../api/client";

const inputStyle = {
  backgroundColor: "var(--color-bg)",
  borderColor: "var(--color-border)",
  color: "var(--color-text)",
};

const modeOptions: { value: MigrationMode; label: string; description: string }[] = [
  {
    value: "clone_follow_switchover",
    label: "Full Migration",
    description: "Initial copy + CDC streaming + switchover. Complete zero-downtime migration.",
  },
  {
    value: "clone_and_follow",
    label: "Copy + Stream",
    description: "Initial copy + CDC streaming. Stays in sync, switchover done manually later.",
  },
  {
    value: "clone_only",
    label: "Copy Only",
    description: "One-time snapshot copy. No CDC streaming or switchover.",
  },
];

export function CreateMigrationPage() {
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [sourceClusterId, setSourceClusterId] = useState("");
  const [sourceNodeId, setSourceNodeId] = useState("");
  const [destClusterId, setDestClusterId] = useState("");
  const [destNodeId, setDestNodeId] = useState("");
  const [mode, setMode] = useState<MigrationMode>("clone_and_follow");
  const [fallback, setFallback] = useState(false);
  const [name, setName] = useState("");
  const [migrationId, setMigrationId] = useState("");
  const [copyWorkers, setCopyWorkers] = useState("4");

  useEffect(() => {
    fetchClusters()
      .then((data) => setClusters(data || []))
      .finally(() => setLoading(false));
  }, []);

  const sourceCluster = clusters.find((c) => c.id === sourceClusterId);
  const destCluster = clusters.find((c) => c.id === destClusterId);

  function autoName() {
    if (sourceClusterId && destClusterId && !name) {
      setName(`${sourceClusterId} → ${destClusterId}`);
    }
    if (sourceClusterId && destClusterId && !migrationId) {
      setMigrationId(`${sourceClusterId}-to-${destClusterId}`);
    }
  }

  function canProceed(): boolean {
    switch (step) {
      case 0: return !!sourceClusterId && !!sourceNodeId && !!destClusterId && !!destNodeId;
      case 1: return !!mode;
      case 2: return !!name && !!migrationId;
      default: return false;
    }
  }

  async function handleSubmit() {
    setSubmitting(true);
    setError(null);
    try {
      const req: CreateMigrationRequest = {
        id: migrationId,
        name,
        source_cluster_id: sourceClusterId,
        dest_cluster_id: destClusterId,
        source_node_id: sourceNodeId,
        dest_node_id: destNodeId,
        mode,
        fallback,
        copy_workers: parseInt(copyWorkers) || 4,
      };
      await createMigration(req);
      navigate("/migration");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to create migration");
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  const steps = ["Clusters", "Strategy", "Review"];

  return (
    <div className="space-y-6 max-w-3xl">
      <div className="flex items-center gap-3">
        <button
          onClick={() => navigate("/migration")}
          className="p-1.5 rounded-md transition-colors hover:bg-white/5"
          style={{ color: "var(--color-text-muted)" }}
        >
          <ArrowLeft className="w-4 h-4" />
        </button>
        <div>
          <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
            Create Migration
          </h2>
          <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
            Step {step + 1} of {steps.length}: {steps[step]}
          </p>
        </div>
      </div>

      <div className="flex gap-1">
        {steps.map((s, i) => (
          <div key={s} className="flex items-center gap-1">
            <div
              className="h-1.5 rounded-full transition-all"
              style={{
                width: i <= step ? "3rem" : "2rem",
                backgroundColor: i <= step ? "var(--color-accent)" : "var(--color-border)",
              }}
            />
          </div>
        ))}
      </div>

      {error && (
        <div className="flex items-center gap-2 text-xs text-red-400">
          <XCircle className="w-3.5 h-3.5" /> {error}
        </div>
      )}

      <div
        className="rounded-lg border p-6"
        style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
      >
        {step === 0 && (
          <ClusterStep
            clusters={clusters}
            sourceClusterId={sourceClusterId}
            sourceNodeId={sourceNodeId}
            destClusterId={destClusterId}
            destNodeId={destNodeId}
            onSourceCluster={(id) => { setSourceClusterId(id); setSourceNodeId(""); }}
            onSourceNode={setSourceNodeId}
            onDestCluster={(id) => { setDestClusterId(id); setDestNodeId(""); }}
            onDestNode={setDestNodeId}
          />
        )}

        {step === 1 && (
          <StrategyStep
            mode={mode}
            fallback={fallback}
            copyWorkers={copyWorkers}
            onMode={setMode}
            onFallback={setFallback}
            onCopyWorkers={setCopyWorkers}
          />
        )}

        {step === 2 && (
          <ReviewStep
            migrationId={migrationId}
            name={name}
            sourceCluster={sourceCluster}
            destCluster={destCluster}
            sourceNodeId={sourceNodeId}
            destNodeId={destNodeId}
            mode={mode}
            fallback={fallback}
            copyWorkers={copyWorkers}
            onId={setMigrationId}
            onName={setName}
          />
        )}
      </div>

      <div className="flex items-center justify-between">
        <button
          onClick={() => setStep((s) => Math.max(0, s - 1))}
          disabled={step === 0}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm transition-colors disabled:opacity-30"
          style={{ color: "var(--color-text-secondary)" }}
        >
          <ArrowLeft className="w-4 h-4" /> Back
        </button>
        {step < steps.length - 1 ? (
          <button
            onClick={() => { if (step === 0) autoName(); setStep((s) => s + 1); }}
            disabled={!canProceed()}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
            style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
          >
            Next <ArrowRight className="w-4 h-4" />
          </button>
        ) : (
          <button
            onClick={handleSubmit}
            disabled={submitting || !canProceed()}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
            style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
          >
            {submitting ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
            Create Migration
          </button>
        )}
      </div>
    </div>
  );
}

function ClusterStep({
  clusters, sourceClusterId, sourceNodeId, destClusterId, destNodeId,
  onSourceCluster, onSourceNode, onDestCluster, onDestNode,
}: {
  clusters: Cluster[];
  sourceClusterId: string; sourceNodeId: string;
  destClusterId: string; destNodeId: string;
  onSourceCluster: (id: string) => void; onSourceNode: (id: string) => void;
  onDestCluster: (id: string) => void; onDestNode: (id: string) => void;
}) {
  if (clusters.length < 2) {
    return (
      <div className="text-center py-8">
        <Database className="w-10 h-10 mx-auto mb-3" style={{ color: "var(--color-text-muted)" }} />
        <p className="font-medium" style={{ color: "var(--color-text)" }}>
          At least two clusters required
        </p>
        <p className="text-sm mt-1" style={{ color: "var(--color-text-muted)" }}>
          Register your source and destination clusters first.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <ClusterPicker
        label="Source Cluster"
        description="The cluster to migrate data from"
        clusters={clusters}
        selectedClusterId={sourceClusterId}
        selectedNodeId={sourceNodeId}
        onCluster={onSourceCluster}
        onNode={onSourceNode}
        excludeClusterId={destClusterId}
      />

      <div className="flex items-center justify-center">
        <ChevronRight className="w-5 h-5" style={{ color: "var(--color-text-muted)" }} />
      </div>

      <ClusterPicker
        label="Destination Cluster"
        description="The cluster to migrate data to"
        clusters={clusters}
        selectedClusterId={destClusterId}
        selectedNodeId={destNodeId}
        onCluster={onDestCluster}
        onNode={onDestNode}
        excludeClusterId={sourceClusterId}
      />
    </div>
  );
}

function ClusterPicker({
  label, description, clusters, selectedClusterId, selectedNodeId,
  onCluster, onNode, excludeClusterId,
}: {
  label: string; description: string;
  clusters: Cluster[]; selectedClusterId: string; selectedNodeId: string;
  onCluster: (id: string) => void; onNode: (id: string) => void;
  excludeClusterId?: string;
}) {
  const available = clusters.filter((c) => c.id !== excludeClusterId);
  const cluster = clusters.find((c) => c.id === selectedClusterId);

  return (
    <div className="space-y-3">
      <div>
        <p className="text-sm font-medium" style={{ color: "var(--color-text)" }}>{label}</p>
        <p className="text-xs" style={{ color: "var(--color-text-muted)" }}>{description}</p>
      </div>

      <div className="grid grid-cols-2 gap-2">
        {available.map((c) => (
          <button
            key={c.id}
            type="button"
            onClick={() => {
              onCluster(c.id);
              const primary = c.nodes.find((n: ClusterNode) => n.role === "primary");
              if (primary) onNode(primary.id);
              else if (c.nodes.length === 1) onNode(c.nodes[0].id);
            }}
            className="rounded-lg border p-3 text-left transition-colors"
            style={{
              backgroundColor: selectedClusterId === c.id ? "var(--color-accent)" + "15" : "var(--color-bg)",
              borderColor: selectedClusterId === c.id ? "var(--color-accent)" : "var(--color-border)",
            }}
          >
            <div className="flex items-center gap-2">
              <Database className="w-4 h-4" style={{ color: selectedClusterId === c.id ? "var(--color-accent)" : "var(--color-text-muted)" }} />
              <span className="text-sm font-medium" style={{ color: "var(--color-text)" }}>{c.name}</span>
            </div>
            <div className="text-xs mt-1" style={{ color: "var(--color-text-muted)" }}>
              {c.nodes.length} node{c.nodes.length !== 1 ? "s" : ""}
              {c.nodes[0] && <> &middot; {c.nodes[0].host}:{c.nodes[0].port}</>}
            </div>
          </button>
        ))}
      </div>

      {cluster && cluster.nodes.length > 1 && (
        <div>
          <p className="text-xs mb-1.5" style={{ color: "var(--color-text-muted)" }}>Select node</p>
          <select
            className="w-full rounded-md border px-3 py-2 text-sm"
            style={inputStyle}
            value={selectedNodeId}
            onChange={(e) => onNode(e.target.value)}
          >
            <option value="">Select a node...</option>
            {cluster.nodes.map((n: ClusterNode) => (
              <option key={n.id} value={n.id}>
                {n.id} ({n.role}) &mdash; {n.host}:{n.port}
              </option>
            ))}
          </select>
        </div>
      )}
    </div>
  );
}

function StrategyStep({
  mode, fallback, copyWorkers, onMode, onFallback, onCopyWorkers,
}: {
  mode: MigrationMode; fallback: boolean; copyWorkers: string;
  onMode: (m: MigrationMode) => void;
  onFallback: (v: boolean) => void;
  onCopyWorkers: (v: string) => void;
}) {
  return (
    <div className="space-y-6">
      <div>
        <p className="text-sm font-medium mb-3" style={{ color: "var(--color-text)" }}>
          Migration Strategy
        </p>
        <div className="space-y-2">
          {modeOptions.map((opt) => (
            <button
              key={opt.value}
              type="button"
              onClick={() => onMode(opt.value)}
              className="w-full rounded-lg border p-4 text-left transition-colors"
              style={{
                backgroundColor: mode === opt.value ? "var(--color-accent)" + "15" : "var(--color-bg)",
                borderColor: mode === opt.value ? "var(--color-accent)" : "var(--color-border)",
              }}
            >
              <div className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
                {opt.label}
              </div>
              <div className="text-xs mt-1" style={{ color: "var(--color-text-muted)" }}>
                {opt.description}
              </div>
            </button>
          ))}
        </div>
      </div>

      {mode !== "clone_only" && (
        <div
          className="border-t pt-4"
          style={{ borderColor: "var(--color-border)" }}
        >
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={fallback}
              onChange={(e) => onFallback(e.target.checked)}
              className="rounded"
            />
            <div>
              <p className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
                Enable fallback replication
              </p>
              <p className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                After switchover, sets up reverse replication from destination back to source for failback.
              </p>
            </div>
          </label>
        </div>
      )}

      <div
        className="border-t pt-4"
        style={{ borderColor: "var(--color-border)" }}
      >
        <label className="block">
          <span className="text-xs" style={{ color: "var(--color-text-secondary)" }}>
            Parallel copy workers
          </span>
          <input
            type="number"
            min="1"
            max="32"
            className="mt-1 w-24 rounded-md border px-3 py-2 text-sm"
            style={inputStyle}
            value={copyWorkers}
            onChange={(e) => onCopyWorkers(e.target.value)}
          />
        </label>
        <p className="text-xs mt-1" style={{ color: "var(--color-text-muted)" }}>
          Number of tables copied in parallel during initial load.
        </p>
      </div>
    </div>
  );
}

function ReviewStep({
  migrationId, name, sourceCluster, destCluster, sourceNodeId, destNodeId,
  mode, fallback, copyWorkers, onId, onName,
}: {
  migrationId: string; name: string;
  sourceCluster?: Cluster; destCluster?: Cluster;
  sourceNodeId: string; destNodeId: string;
  mode: MigrationMode; fallback: boolean; copyWorkers: string;
  onId: (v: string) => void; onName: (v: string) => void;
}) {
  const modeLabelMap: Record<MigrationMode, string> = {
    clone_only: "Copy Only",
    clone_and_follow: "Copy + Stream",
    clone_follow_switchover: "Full Migration",
  };

  const srcNode = sourceCluster?.nodes.find((n) => n.id === sourceNodeId);
  const dstNode = destCluster?.nodes.find((n) => n.id === destNodeId);

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs mb-1" style={{ color: "var(--color-text-secondary)" }}>
            Migration ID
          </label>
          <input
            className="w-full rounded-md border px-3 py-2 text-sm"
            style={inputStyle}
            placeholder="prod-to-staging"
            value={migrationId}
            onChange={(e) => onId(e.target.value)}
            required
          />
        </div>
        <div>
          <label className="block text-xs mb-1" style={{ color: "var(--color-text-secondary)" }}>
            Name
          </label>
          <input
            className="w-full rounded-md border px-3 py-2 text-sm"
            style={inputStyle}
            placeholder="Production to Staging"
            value={name}
            onChange={(e) => onName(e.target.value)}
            required
          />
        </div>
      </div>

      <div
        className="border-t pt-4 space-y-3"
        style={{ borderColor: "var(--color-border)" }}
      >
        <p className="text-xs font-medium uppercase tracking-widest" style={{ color: "var(--color-text-muted)" }}>
          Summary
        </p>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>Source</span>
            <p style={{ color: "var(--color-text)" }}>
              {sourceCluster?.name || sourceNodeId}
            </p>
            {srcNode && (
              <p className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                {srcNode.host}:{srcNode.port} ({srcNode.role})
              </p>
            )}
          </div>
          <div>
            <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>Destination</span>
            <p style={{ color: "var(--color-text)" }}>
              {destCluster?.name || destNodeId}
            </p>
            {dstNode && (
              <p className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                {dstNode.host}:{dstNode.port} ({dstNode.role})
              </p>
            )}
          </div>
          <div>
            <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>Strategy</span>
            <p style={{ color: "var(--color-text)" }}>{modeLabelMap[mode]}</p>
          </div>
          <div>
            <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>Options</span>
            <p style={{ color: "var(--color-text)" }}>
              {copyWorkers} workers{fallback ? " + fallback" : ""}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
