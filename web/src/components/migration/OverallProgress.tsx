import type { Snapshot } from "../../types/metrics";

export function OverallProgress({ snapshot }: { snapshot: Snapshot }) {
  const total = snapshot.tables_total;
  const copied = snapshot.tables_copied;
  const pct = total > 0 ? (copied / total) * 100 : 0;

  return (
    <div className="rounded-lg border p-4"
      style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <div className="flex justify-between text-sm mb-2.5">
        <span style={{ color: "var(--color-text-secondary)" }}>Overall Progress</span>
        <span className="tabular-nums" style={{ color: "var(--color-text)" }}>
          {copied}/{total} tables
          <span className="ml-2" style={{ color: "var(--color-text-muted)" }}>({pct.toFixed(1)}%)</span>
        </span>
      </div>
      <div className="w-full rounded-full h-2" style={{ backgroundColor: "var(--color-border)" }}>
        <div
          className="h-2 rounded-full transition-all duration-500"
          style={{ width: `${pct}%`, backgroundColor: "var(--color-accent)" }}
        />
      </div>
    </div>
  );
}
