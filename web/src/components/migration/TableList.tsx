import type { TableProgress } from "../../types/metrics";

function formatCount(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return `${n}`;
}

function formatBytes(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KB`;
  return `${bytes} B`;
}

const statusStyles: Record<string, { color: string; bg: string; label: string }> = {
  pending: { color: "#6b7280", bg: "#1f2937", label: "Pending" },
  copying: { color: "#eab308", bg: "#422006", label: "Copying" },
  copied: { color: "#22c55e", bg: "#052e16", label: "Done" },
  streaming: { color: "#60a5fa", bg: "#172554", label: "Live" },
};

export function TableList({ tables }: { tables: TableProgress[] }) {
  if (!tables || tables.length === 0) {
    return (
      <div className="rounded-lg border p-6 text-center"
        style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)", color: "var(--color-text-muted)" }}>
        No table data available
      </div>
    );
  }

  return (
    <div className="rounded-lg border overflow-hidden"
      style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <div className="px-4 py-3 border-b" style={{ borderColor: "var(--color-border)" }}>
        <h3 className="text-xs font-medium uppercase tracking-wider"
          style={{ color: "var(--color-text-muted)" }}>
          Tables ({tables.length})
        </h3>
      </div>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left" style={{ borderColor: "var(--color-border)" }}>
            <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wider"
              style={{ color: "var(--color-text-muted)" }}>Table</th>
            <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wider"
              style={{ color: "var(--color-text-muted)" }}>Status</th>
            <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wider"
              style={{ color: "var(--color-text-muted)" }}>Rows</th>
            <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wider"
              style={{ color: "var(--color-text-muted)" }}>Size</th>
            <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wider w-48"
              style={{ color: "var(--color-text-muted)" }}>Progress</th>
          </tr>
        </thead>
        <tbody>
          {tables.map((t) => {
            const st = statusStyles[t.status] || statusStyles.pending;
            return (
              <tr key={`${t.schema}.${t.name}`}
                className="border-b last:border-0 transition-colors"
                style={{ borderColor: "var(--color-border-subtle)" }}
                onMouseEnter={(e) => e.currentTarget.style.backgroundColor = "var(--color-surface-hover)"}
                onMouseLeave={(e) => e.currentTarget.style.backgroundColor = "transparent"}>
                <td className="px-4 py-2.5 font-mono text-xs" style={{ color: "var(--color-text)" }}>
                  <span style={{ color: "var(--color-text-muted)" }}>{t.schema}.</span>{t.name}
                </td>
                <td className="px-4 py-2.5">
                  <span className="inline-block text-[10px] font-medium px-2 py-0.5 rounded"
                    style={{ backgroundColor: st.bg, color: st.color }}>
                    {st.label}
                  </span>
                </td>
                <td className="px-4 py-2.5 font-mono text-xs tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                  {t.status === "streaming" ? "—" : `${formatCount(t.rows_copied)} / ${formatCount(t.rows_total)}`}
                </td>
                <td className="px-4 py-2.5 font-mono text-xs tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                  {formatBytes(t.size_bytes)}
                </td>
                <td className="px-4 py-2.5">
                  {t.status === "streaming" ? (
                    <span className="text-[10px] font-medium" style={{ color: "#60a5fa" }}>⟳ streaming</span>
                  ) : (
                    <div className="flex items-center gap-2">
                      <div className="flex-1 rounded-full h-1.5" style={{ backgroundColor: "var(--color-border)" }}>
                        <div className="h-1.5 rounded-full transition-all duration-500"
                          style={{ width: `${t.percent}%`, backgroundColor: st.color }} />
                      </div>
                      <span className="text-[10px] tabular-nums w-8 text-right"
                        style={{ color: "var(--color-text-muted)" }}>
                        {t.percent.toFixed(0)}%
                      </span>
                    </div>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
