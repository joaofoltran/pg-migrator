import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";
import type { Snapshot } from "../../types/metrics";

function formatBytes(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KB`;
  return `${bytes} B`;
}

export function LagChart({ history }: { history: Snapshot[] }) {
  const data = history
    .map((s) => ({
      time: new Date(s.timestamp).toLocaleTimeString(),
      lag: s.lag_bytes,
    }))
    .slice(-60);

  return (
    <div className="rounded-lg border p-4"
      style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <h3 className="text-xs font-medium uppercase tracking-wider mb-3"
        style={{ color: "var(--color-text-muted)" }}>
        Replication Lag
      </h3>
      {data.length === 0 ? (
        <div className="h-[180px] flex items-center justify-center text-sm"
          style={{ color: "var(--color-text-muted)" }}>
          Waiting for data...
        </div>
      ) : (
        <ResponsiveContainer width="100%" height={180}>
          <LineChart data={data}>
            <XAxis dataKey="time" stroke="#2a2a3a" fontSize={10} tickLine={false} axisLine={false} />
            <YAxis stroke="#2a2a3a" fontSize={10} tickFormatter={formatBytes} tickLine={false} axisLine={false} width={60} />
            <Tooltip
              contentStyle={{
                backgroundColor: "#1a1a25",
                border: "1px solid #2a2a3a",
                borderRadius: "8px",
                color: "#e4e4ef",
                fontSize: "12px",
              }}
              formatter={(value: number) => [formatBytes(value), "Lag"]}
            />
            <Line
              type="monotone"
              dataKey="lag"
              stroke="var(--color-accent)"
              strokeWidth={1.5}
              dot={false}
              animationDuration={300}
            />
          </LineChart>
        </ResponsiveContainer>
      )}
    </div>
  );
}
