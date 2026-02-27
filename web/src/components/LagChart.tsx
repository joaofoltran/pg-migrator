import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import type { Snapshot } from "../types/metrics";

function formatBytes(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KB`;
  return `${bytes} B`;
}

interface LagPoint {
  time: string;
  lag: number;
}

export function LagChart({ history }: { history: Snapshot[] }) {
  const data: LagPoint[] = history.map((s) => ({
    time: new Date(s.timestamp).toLocaleTimeString(),
    lag: s.lag_bytes,
  }));

  // Show last 60 data points.
  const visible = data.slice(-60);

  return (
    <div className="bg-gray-800 rounded-lg p-4">
      <h3 className="text-sm text-gray-400 mb-3">Replication Lag</h3>
      {visible.length === 0 ? (
        <div className="text-gray-500 text-sm">Waiting for data...</div>
      ) : (
        <ResponsiveContainer width="100%" height={200}>
          <LineChart data={visible}>
            <XAxis
              dataKey="time"
              stroke="#4B5563"
              fontSize={10}
              tickLine={false}
            />
            <YAxis
              stroke="#4B5563"
              fontSize={10}
              tickFormatter={formatBytes}
              tickLine={false}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "#1F2937",
                border: "1px solid #374151",
                borderRadius: "8px",
                color: "#E5E7EB",
              }}
              formatter={(value: number) => [formatBytes(value), "Lag"]}
            />
            <Line
              type="monotone"
              dataKey="lag"
              stroke="#8B5CF6"
              strokeWidth={2}
              dot={false}
              animationDuration={300}
            />
          </LineChart>
        </ResponsiveContainer>
      )}
    </div>
  );
}
