import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  ArrowLeftRight,
  Plus,
  Trash2,
  Loader2,
  ChevronRight,
  WifiOff,
  RefreshCw,
  Play,
  Square,
  CheckCircle2,
  XCircle,
  Clock,
  Zap,
} from "lucide-react";
import type { Migration, MigrationStatus } from "../types/migration";
import { fetchMigrations, removeMigration } from "../api/client";

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

export function MigrationsListPage() {
  const navigate = useNavigate();
  const [migrations, setMigrations] = useState<Migration[]>([]);
  const [loading, setLoading] = useState(true);
  const [apiDown, setApiDown] = useState(false);

  async function load() {
    try {
      const data = await fetchMigrations();
      setMigrations(data || []);
      setApiDown(false);
    } catch {
      setMigrations([]);
      setApiDown(true);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
    const interval = setInterval(load, 5000);
    return () => clearInterval(interval);
  }, []);

  async function handleRemove(e: React.MouseEvent, id: string, status: string) {
    e.stopPropagation();
    const active = status === "running" || status === "streaming" || status === "switchover";
    if (active && !confirm("This migration appears to be running. Force delete it?")) return;
    try {
      await removeMigration(id, active);
      setMigrations((prev) => prev.filter((m) => m.id !== id));
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : "Failed to remove migration");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
            Migrations
          </h2>
          <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
            Online database migrations & CDC streaming
          </p>
        </div>
        <button
          onClick={() => navigate("/migration/new")}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
        >
          <Plus className="w-4 h-4" />
          Create Migration
        </button>
      </div>

      {apiDown && (
        <div
          className="rounded-lg border p-8 text-center"
          style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
        >
          <WifiOff className="w-8 h-8 mx-auto mb-3" style={{ color: "var(--color-text-muted)" }} />
          <p className="font-medium" style={{ color: "var(--color-text)" }}>Unable to reach API</p>
          <p className="text-sm mt-1" style={{ color: "var(--color-text-muted)" }}>
            Check that the pgmanager process is running.
          </p>
          <button
            onClick={() => { setLoading(true); setApiDown(false); load(); }}
            className="mt-4 flex items-center gap-2 mx-auto px-4 py-2 rounded-lg text-sm transition-colors hover:bg-white/5"
            style={{ color: "var(--color-accent)" }}
          >
            <RefreshCw className="w-4 h-4" /> Retry
          </button>
        </div>
      )}

      {migrations.length === 0 && !apiDown ? (
        <div
          className="rounded-lg border p-12 text-center"
          style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
        >
          <ArrowLeftRight
            className="w-12 h-12 mx-auto mb-4"
            style={{ color: "var(--color-text-muted)" }}
          />
          <h3 className="text-sm font-medium mb-1" style={{ color: "var(--color-text)" }}>
            No migrations yet
          </h3>
          <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
            Create your first migration to move data between clusters.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {migrations.map((m) => {
            const sc = statusConfig[m.status] || statusConfig.created;
            const StatusIcon = sc.icon;
            return (
              <div
                key={m.id}
                className="rounded-lg border cursor-pointer transition-colors hover:border-[var(--color-accent)]/30"
                style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
                onClick={() => navigate(`/migration/${m.id}`)}
              >
                <div className="flex items-center gap-3 p-4">
                  <div
                    className="w-8 h-8 rounded-lg flex items-center justify-center"
                    style={{ backgroundColor: sc.color + "20", color: sc.color }}
                  >
                    <StatusIcon className="w-4 h-4" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
                      {m.name}
                    </div>
                    <div className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                      {m.source_cluster_id} &rarr; {m.dest_cluster_id}
                      {m.phase && <> &middot; {m.phase}</>}
                    </div>
                  </div>
                  <span
                    className="text-[10px] px-2 py-0.5 rounded-full font-medium"
                    style={{ backgroundColor: sc.color + "20", color: sc.color }}
                  >
                    {sc.label}
                  </span>
                  {m.tables_total > 0 && (
                    <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                      {m.tables_copied}/{m.tables_total} tables
                    </span>
                  )}
                  <button
                      onClick={(e) => handleRemove(e, m.id, m.status)}
                      className="p-1.5 rounded-md hover:bg-red-500/10 transition-colors"
                    >
                      <Trash2 className="w-4 h-4 text-red-400" />
                    </button>
                  <ChevronRight className="w-4 h-4" style={{ color: "var(--color-text-muted)" }} />
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
