import { useEffect, useRef, useState } from "react";
import type { LogEntry } from "../../types/metrics";
import { fetchLogs } from "../../api/client";

const levelColors: Record<string, string> = {
  debug: "#6b7280",
  info: "#60a5fa",
  warn: "#facc15",
  error: "#f87171",
};

export function LogViewer() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const entries = await fetchLogs();
        if (entries) setLogs(entries);
      } catch { /* ignore */ }
    }, 2000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div className="rounded-lg border p-4"
      style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <h3 className="text-xs font-medium uppercase tracking-wider mb-3"
        style={{ color: "var(--color-text-muted)" }}>
        Logs
      </h3>
      <div ref={containerRef}
        className="font-mono text-[11px] leading-5 space-y-px max-h-[180px] overflow-y-auto">
        {logs.length === 0 ? (
          <div className="flex items-center justify-center h-[180px] text-sm"
            style={{ color: "var(--color-text-muted)" }}>
            No log entries yet
          </div>
        ) : (
          logs.map((entry, i) => (
            <div key={i} className="flex gap-3 px-1 py-0.5 rounded hover:bg-white/[0.02]">
              <span style={{ color: "var(--color-text-muted)" }} className="shrink-0">
                {new Date(entry.time).toLocaleTimeString()}
              </span>
              <span className="shrink-0 w-8 font-medium"
                style={{ color: levelColors[entry.level] || "#6b7280" }}>
                {entry.level.toUpperCase().slice(0, 3)}
              </span>
              <span style={{ color: "var(--color-text-secondary)" }}>{entry.message}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
