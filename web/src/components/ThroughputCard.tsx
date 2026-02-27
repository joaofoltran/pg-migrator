import type { Snapshot } from "../types/metrics";

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

export function ThroughputCard({ snapshot }: { snapshot: Snapshot }) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
      <MetricCard
        label="Rows/sec"
        value={snapshot.rows_per_sec.toFixed(0)}
        color="text-blue-400"
      />
      <MetricCard
        label="Bytes/sec"
        value={formatBytes(snapshot.bytes_per_sec)}
        color="text-green-400"
      />
      <MetricCard
        label="Total Rows"
        value={formatCount(snapshot.total_rows)}
        color="text-purple-400"
      />
      <MetricCard
        label="Total Data"
        value={formatBytes(snapshot.total_bytes)}
        color="text-yellow-400"
      />
    </div>
  );
}

function MetricCard({
  label,
  value,
  color,
}: {
  label: string;
  value: string;
  color: string;
}) {
  return (
    <div className="bg-gray-800 rounded-lg p-4">
      <div className="text-xs text-gray-500 uppercase tracking-wider">
        {label}
      </div>
      <div className={`text-2xl font-bold ${color} mt-1`}>{value}</div>
    </div>
  );
}
