import type { Snapshot } from "../../types/metrics";
import { Rows3, HardDrive, Gauge, Database } from "lucide-react";

function formatBytes(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KB`;
  return `${bytes} B`;
}

function formatCount(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return `${n}`;
}

const cards = [
  { key: "rows_per_sec", label: "Rows/sec", icon: Gauge, format: (v: number) => v.toFixed(0) },
  { key: "bytes_per_sec", label: "Throughput", icon: HardDrive, format: formatBytes },
  { key: "total_rows", label: "Total Rows", icon: Rows3, format: formatCount },
  { key: "total_bytes", label: "Total Data", icon: Database, format: formatBytes },
] as const;

export function MetricCards({ snapshot }: { snapshot: Snapshot }) {
  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
      {cards.map((c) => {
        const value = snapshot[c.key as keyof Snapshot] as number;
        return (
          <div key={c.key} className="rounded-lg border p-4"
            style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
            <div className="flex items-center gap-2 mb-2">
              <c.icon className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
              <span className="text-[11px] font-medium uppercase tracking-wider"
                style={{ color: "var(--color-text-muted)" }}>
                {c.label}
              </span>
            </div>
            <div className="text-xl font-semibold tabular-nums" style={{ color: "var(--color-text)" }}>
              {c.format(value)}
            </div>
          </div>
        );
      })}
    </div>
  );
}
