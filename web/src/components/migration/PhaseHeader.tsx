import type { Snapshot } from "../../types/metrics";
import { Clock, Wifi, WifiOff } from "lucide-react";

const phaseConfig: Record<string, { color: string; bg: string }> = {
  idle: { color: "#6b7280", bg: "#1f2937" },
  connecting: { color: "#eab308", bg: "#422006" },
  schema: { color: "#3b82f6", bg: "#172554" },
  copy: { color: "#a855f7", bg: "#3b0764" },
  streaming: { color: "#22c55e", bg: "#052e16" },
  switchover: { color: "#f97316", bg: "#431407" },
  "switchover-complete": { color: "#16a34a", bg: "#052e16" },
  done: { color: "#16a34a", bg: "#052e16" },
};

function formatElapsed(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}h ${m}m ${s}s`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

interface Props {
  snapshot: Snapshot;
  connected: boolean;
}

export function PhaseHeader({ snapshot, connected }: Props) {
  const phase = phaseConfig[snapshot.phase] || phaseConfig.idle;
  const idle = snapshot.phase === "idle" || snapshot.phase === "";

  return (
    <div className="rounded-lg border p-4 flex items-center justify-between"
      style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <div className="flex items-center gap-4">
        <div className="px-3 py-1.5 rounded-md text-xs font-semibold uppercase tracking-wide"
          style={{ backgroundColor: phase.bg, color: phase.color }}>
          {snapshot.phase || "idle"}
        </div>
        {!idle && (
          <div className="flex items-center gap-1.5 text-sm" style={{ color: "var(--color-text-secondary)" }}>
            <Clock className="w-3.5 h-3.5" />
            <span>{formatElapsed(snapshot.elapsed_sec)}</span>
          </div>
        )}
        {snapshot.lag_formatted && !idle && (
          <div className="text-sm" style={{ color: "var(--color-text-secondary)" }}>
            Lag: <span className="font-mono" style={{ color: "var(--color-text)" }}>{snapshot.lag_formatted}</span>
          </div>
        )}
      </div>
      <div className="flex items-center gap-2 text-xs" style={{ color: "var(--color-text-muted)" }}>
        {connected ? (
          <><Wifi className="w-3.5 h-3.5 text-emerald-500" /> Connected</>
        ) : (
          <><WifiOff className="w-3.5 h-3.5 text-red-400" /> Disconnected</>
        )}
      </div>
    </div>
  );
}
