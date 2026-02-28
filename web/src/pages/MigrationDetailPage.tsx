import { useEffect, useState, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  ArrowLeft,
  Play,
  Square,
  ArrowLeftRight,
  Loader2,
  CheckCircle2,
  XCircle,
  Clock,
  Zap,
  Database,
  RefreshCw,
  Trash2,
  Table2,
} from "lucide-react";
import type { Migration, MigrationStatus } from "../types/migration";
import type { TableProgress } from "../types/metrics";
import {
  fetchMigration,
  startMigration,
  stopMigration,
  switchoverMigration,
  removeMigration,
} from "../api/client";

const statusConfig: Record<
  MigrationStatus,
  { label: string; color: string; icon: React.ComponentType<{ className?: string }> }
> = {
  created: { label: "Created", color: "#6b7280", icon: Clock },
  running: { label: "Running", color: "#3b82f6", icon: Play },
  streaming: { label: "Streaming", color: "#8b5cf6", icon: Zap },
  switchover: { label: "Switchover", color: "#f59e0b", icon: ArrowLeftRight },
  completed: { label: "Completed", color: "#10b981", icon: CheckCircle2 },
  failed: { label: "Failed", color: "#ef4444", icon: XCircle },
  stopped: { label: "Stopped", color: "#6b7280", icon: Square },
};

export function MigrationDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [migration, setMigration] = useState<Migration | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const data = await fetchMigration(id);
      setMigration(data);
      setError(null);
    } catch {
      if (!migration) setError("Migration not found");
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    load();
    const isActive = migration?.status === "running" || migration?.status === "streaming" || migration?.status === "switchover";
    const interval = setInterval(load, isActive ? 2000 : 10000);
    return () => clearInterval(interval);
  }, [load, migration?.status]);

  async function handleAction(action: () => Promise<void>) {
    setActionLoading(true);
    setError(null);
    try {
      await action();
      await load();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setActionLoading(false);
    }
  }

  async function handleDelete() {
    if (!id) return;
    const active = migration?.status === "running" || migration?.status === "streaming" || migration?.status === "switchover";
    const msg = active
      ? "This migration appears to be running. Force delete it? This will stop it and cannot be undone."
      : "Delete this migration? This cannot be undone.";
    if (!confirm(msg)) return;
    try {
      await removeMigration(id, active);
      navigate("/migration");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to delete");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  if (!migration) {
    return (
      <div className="space-y-4 max-w-4xl">
        <button onClick={() => navigate("/migration")} className="flex items-center gap-2 text-sm" style={{ color: "var(--color-text-muted)" }}>
          <ArrowLeft className="w-4 h-4" /> Back to migrations
        </button>
        <div className="rounded-lg border p-8 text-center" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <p style={{ color: "var(--color-text)" }}>Migration not found</p>
        </div>
      </div>
    );
  }

  const sc = statusConfig[migration.status] || statusConfig.created;
  const StatusIcon = sc.icon;
  const isActive = migration.status === "running" || migration.status === "streaming" || migration.status === "switchover";
  const canStart = migration.status === "created" || migration.status === "failed" || migration.status === "stopped";
  const canStop = isActive;
  const canSwitchover = migration.status === "streaming" && migration.mode !== "clone_only";
  const canDelete = true;

  const phase = migration.live_phase || migration.phase;
  const lsn = migration.live_lsn || migration.confirmed_lsn;
  const tablesTotal = migration.live_tables_total || migration.tables_total;
  const tablesCopied = migration.live_tables_copied || migration.tables_copied;
  const liveTables = migration.live_tables || [];
  const rowsPerSec = migration.live_rows_per_sec || 0;
  const bytesPerSec = migration.live_bytes_per_sec || 0;

  const totalRowsAll = liveTables.reduce((s, t) => s + t.rows_total, 0);
  const copiedRowsAll = liveTables.reduce((s, t) => s + t.rows_copied, 0);
  const copyPercent = liveTables.length > 0 && totalRowsAll > 0
    ? Math.round((copiedRowsAll / totalRowsAll) * 100)
    : tablesTotal > 0
      ? Math.round((tablesCopied / tablesTotal) * 100)
      : 0;

  const modeLabelMap: Record<string, string> = {
    clone_only: "Copy Only",
    clone_and_follow: "Copy + Stream",
    clone_follow_switchover: "Full Migration",
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center gap-3">
        <button
          onClick={() => navigate("/migration")}
          className="p-1.5 rounded-md transition-colors hover:bg-white/5"
          style={{ color: "var(--color-text-muted)" }}
        >
          <ArrowLeft className="w-4 h-4" />
        </button>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
              {migration.name}
            </h2>
            <span
              className="text-[10px] px-2 py-0.5 rounded-full font-medium flex items-center gap-1"
              style={{ backgroundColor: sc.color + "20", color: sc.color }}
            >
              <StatusIcon className="w-3 h-3" />
              {sc.label}
            </span>
          </div>
          <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
            {migration.id}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {canStart && (
            <button
              onClick={() => handleAction(() => startMigration(migration.id))}
              disabled={actionLoading}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
              style={{ backgroundColor: "#10b981", color: "#fff" }}
            >
              {actionLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />}
              Start
            </button>
          )}
          {canStop && (
            <button
              onClick={() => handleAction(() => stopMigration(migration.id))}
              disabled={actionLoading}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
              style={{ backgroundColor: "#ef4444", color: "#fff" }}
            >
              {actionLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Square className="w-3.5 h-3.5" />}
              Stop
            </button>
          )}
          {canSwitchover && (
            <button
              onClick={() => { if (confirm("Initiate switchover?")) handleAction(() => switchoverMigration(migration.id)); }}
              disabled={actionLoading}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
              style={{ backgroundColor: "#f59e0b", color: "#fff" }}
            >
              <ArrowLeftRight className="w-3.5 h-3.5" />
              Switchover
            </button>
          )}
          {canDelete && (
            <button
              onClick={handleDelete}
              className="p-1.5 rounded-md hover:bg-red-500/10 transition-colors"
            >
              <Trash2 className="w-4 h-4 text-red-400" />
            </button>
          )}
          <button
            onClick={load}
            className="p-1.5 rounded-md transition-colors hover:bg-white/5"
            style={{ color: "var(--color-text-muted)" }}
          >
            <RefreshCw className="w-4 h-4" />
          </button>
        </div>
      </div>

      {error && (
        <div className="flex items-center gap-2 text-xs text-red-400 rounded-lg border border-red-400/20 p-3" style={{ backgroundColor: "var(--color-surface)" }}>
          <XCircle className="w-3.5 h-3.5 shrink-0" /> {error}
        </div>
      )}

      {migration.error_message && (
        <div className="rounded-lg border border-red-400/20 p-4" style={{ backgroundColor: "#ef444410" }}>
          <p className="text-sm font-medium text-red-400 mb-1">Error</p>
          <p className="text-xs font-mono text-red-300">{migration.error_message}</p>
        </div>
      )}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard label="Status" value={sc.label} icon={StatusIcon} color={sc.color} />
        <StatCard label="Phase" value={phase || "—"} icon={Zap} />
        <StatCard label="Tables" value={tablesTotal > 0 ? `${tablesCopied}/${tablesTotal}` : "—"} icon={Database} />
        <StatCard label="LSN" value={lsn || "—"} icon={CheckCircle2} mono />
      </div>

      {tablesTotal > 0 && (
        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs font-medium" style={{ color: "var(--color-text)" }}>Copy Progress</span>
            <div className="flex items-center gap-3">
              {rowsPerSec > 0 && (
                <span className="text-[10px] font-mono" style={{ color: "var(--color-text-muted)" }}>
                  {formatNumber(rowsPerSec)} rows/s
                </span>
              )}
              {bytesPerSec > 0 && (
                <span className="text-[10px] font-mono" style={{ color: "var(--color-text-muted)" }}>
                  {formatBytes(bytesPerSec)}/s
                </span>
              )}
              <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>{copyPercent}%</span>
            </div>
          </div>
          <div className="h-2 rounded-full" style={{ backgroundColor: "var(--color-border)" }}>
            <div
              className="h-full rounded-full transition-all duration-500"
              style={{ width: `${copyPercent}%`, backgroundColor: "var(--color-accent)" }}
            />
          </div>

          {liveTables.length > 0 && (
            <div className="mt-3 space-y-1.5">
              {liveTables.map((t) => (
                <TableRow key={`${t.schema}.${t.name}`} table={t} />
              ))}
            </div>
          )}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <InfoCard title="Configuration">
          <InfoRow label="Strategy" value={modeLabelMap[migration.mode] || migration.mode} />
          <InfoRow label="Fallback" value={migration.fallback ? "Enabled" : "Disabled"} />
          <InfoRow label="Copy Workers" value={String(migration.copy_workers)} />
          <InfoRow label="Slot Name" value={migration.slot_name} mono />
          <InfoRow label="Publication" value={migration.publication} mono />
        </InfoCard>

        <InfoCard title="Clusters">
          <InfoRow label="Source Cluster" value={migration.source_cluster_id} />
          <InfoRow label="Source Node" value={migration.source_node_id} />
          <InfoRow label="Dest Cluster" value={migration.dest_cluster_id} />
          <InfoRow label="Dest Node" value={migration.dest_node_id} />
        </InfoCard>
      </div>

      <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <p className="text-xs font-medium mb-3" style={{ color: "var(--color-text)" }}>Timeline</p>
        <div className="space-y-2 text-xs">
          <TimelineRow label="Created" time={migration.created_at} />
          <TimelineRow label="Started" time={migration.started_at} />
          <TimelineRow label="Last Updated" time={migration.updated_at} />
          <TimelineRow label="Finished" time={migration.finished_at} />
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value, icon: Icon, color, mono }: {
  label: string; value: string;
  icon: React.ComponentType<{ className?: string; style?: React.CSSProperties }>;
  color?: string; mono?: boolean;
}) {
  return (
    <div className="rounded-lg border p-3" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <div className="flex items-center gap-2 mb-1">
        <Icon className="w-3.5 h-3.5" style={{ color: color || "var(--color-text-muted)" }} />
        <span className="text-[10px] uppercase tracking-wider" style={{ color: "var(--color-text-muted)" }}>{label}</span>
      </div>
      <p className={`text-sm font-medium truncate ${mono ? "font-mono text-xs" : ""}`} style={{ color: "var(--color-text)" }}>
        {value}
      </p>
    </div>
  );
}

function InfoCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <p className="text-xs font-medium mb-3" style={{ color: "var(--color-text)" }}>{title}</p>
      <div className="space-y-2">{children}</div>
    </div>
  );
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between text-xs">
      <span style={{ color: "var(--color-text-muted)" }}>{label}</span>
      <span className={mono ? "font-mono" : ""} style={{ color: "var(--color-text)" }}>{value}</span>
    </div>
  );
}

function TimelineRow({ label, time }: { label: string; time?: string | null }) {
  return (
    <div className="flex items-center justify-between">
      <span style={{ color: "var(--color-text-muted)" }}>{label}</span>
      <span className="font-mono" style={{ color: time ? "var(--color-text)" : "var(--color-text-muted)" }}>
        {time ? new Date(time).toLocaleString() : "—"}
      </span>
    </div>
  );
}

const tableStatusColors: Record<string, { bg: string; text: string; label: string }> = {
  pending:   { bg: "#6b728020", text: "#6b7280", label: "Pending" },
  copying:   { bg: "#3b82f620", text: "#3b82f6", label: "Copying" },
  copied:    { bg: "#10b98120", text: "#10b981", label: "Copied" },
  streaming: { bg: "#8b5cf620", text: "#8b5cf6", label: "Streaming" },
};

function TableRow({ table: t }: { table: TableProgress }) {
  const sc = tableStatusColors[t.status] || tableStatusColors.pending;
  const tableName = t.schema !== "public" ? `${t.schema}.${t.name}` : t.name;
  const pct = Math.round(t.percent);

  return (
    <div className="flex items-center gap-3 py-1.5 px-2 rounded-md" style={{ backgroundColor: "var(--color-bg)" }}>
      <Table2 className="w-3 h-3 shrink-0" style={{ color: "var(--color-text-muted)" }} />
      <span className="text-xs font-mono truncate min-w-0 flex-1" style={{ color: "var(--color-text)" }}>
        {tableName}
      </span>
      <span
        className="text-[9px] px-1.5 py-0.5 rounded font-medium shrink-0"
        style={{ backgroundColor: sc.bg, color: sc.text }}
      >
        {sc.label}
      </span>
      {(t.rows_total > 0 || t.rows_copied > 0) && (
        <span className="text-[10px] font-mono shrink-0 w-24 text-right" style={{ color: "var(--color-text-muted)" }}>
          {t.rows_total > 0 ? `${formatNumber(t.rows_copied)}/${formatNumber(t.rows_total)}` : `${formatNumber(t.rows_copied)} rows`}
        </span>
      )}
      {t.size_bytes > 0 && (
        <span className="text-[10px] font-mono shrink-0 w-16 text-right" style={{ color: "var(--color-text-muted)" }}>
          {formatBytes(t.size_bytes)}
        </span>
      )}
      <div className="w-16 shrink-0">
        <div className="h-1 rounded-full" style={{ backgroundColor: "var(--color-border)" }}>
          <div
            className="h-full rounded-full transition-all duration-300"
            style={{ width: `${pct}%`, backgroundColor: sc.text }}
          />
        </div>
      </div>
      <span className="text-[10px] font-mono shrink-0 w-8 text-right" style={{ color: "var(--color-text-muted)" }}>
        {pct}%
      </span>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`;
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
}
