import type { TableProgress as TableProgressType } from "../types/metrics";

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

const statusColors: Record<string, string> = {
  pending: "text-gray-500",
  copying: "text-yellow-400",
  copied: "text-green-400",
  streaming: "text-blue-400",
};

const statusBg: Record<string, string> = {
  pending: "bg-gray-600",
  copying: "bg-yellow-500",
  copied: "bg-green-500",
  streaming: "bg-blue-500",
};

export function TableProgress({ tables }: { tables: TableProgressType[] }) {
  if (!tables || tables.length === 0) {
    return (
      <div className="bg-gray-800 rounded-lg p-4 text-gray-500">
        No table data available
      </div>
    );
  }

  return (
    <div className="bg-gray-800 rounded-lg overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-700 text-gray-400 text-left">
            <th className="px-4 py-3">Table</th>
            <th className="px-4 py-3">Status</th>
            <th className="px-4 py-3">Rows</th>
            <th className="px-4 py-3">Size</th>
            <th className="px-4 py-3 w-48">Progress</th>
          </tr>
        </thead>
        <tbody>
          {tables.map((t) => (
            <tr
              key={`${t.schema}.${t.name}`}
              className="border-b border-gray-700/50 hover:bg-gray-700/30"
            >
              <td className="px-4 py-2 font-mono text-gray-300">
                {t.schema}.{t.name}
              </td>
              <td className={`px-4 py-2 ${statusColors[t.status]}`}>
                {t.status === "streaming" ? "⟳ live" : t.status}
              </td>
              <td className="px-4 py-2 text-gray-300">
                {t.status === "streaming"
                  ? "—"
                  : `${formatCount(t.rows_copied)}/${formatCount(t.rows_total)}`}
              </td>
              <td className="px-4 py-2 text-gray-300">
                {formatBytes(t.size_bytes)}
              </td>
              <td className="px-4 py-2">
                {t.status === "streaming" ? (
                  <span className="text-blue-400 text-xs">streaming</span>
                ) : (
                  <div className="flex items-center gap-2">
                    <div className="flex-1 bg-gray-700 rounded-full h-2">
                      <div
                        className={`${statusBg[t.status]} h-2 rounded-full transition-all duration-300`}
                        style={{ width: `${t.percent}%` }}
                      />
                    </div>
                    <span className="text-xs text-gray-400 w-12 text-right">
                      {t.percent.toFixed(0)}%
                    </span>
                  </div>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
