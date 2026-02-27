import { useEffect, useRef, useState } from "react";
import type { LogEntry } from "../types/metrics";
import { fetchLogs } from "../api/client";

const levelColors: Record<string, string> = {
  debug: "text-gray-500",
  info: "text-blue-400",
  warn: "text-yellow-400",
  error: "text-red-400",
};

export function LogViewer() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const entries = await fetchLogs();
        if (entries) setLogs(entries);
      } catch {
        // Ignore fetch errors.
      }
    }, 2000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div className="bg-gray-800 rounded-lg p-4">
      <h3 className="text-sm text-gray-400 mb-3">Logs</h3>
      <div
        ref={containerRef}
        className="font-mono text-xs space-y-0.5 max-h-48 overflow-y-auto"
      >
        {logs.length === 0 ? (
          <div className="text-gray-500">No log entries yet</div>
        ) : (
          logs.map((entry, i) => (
            <div key={i} className="flex gap-2">
              <span className="text-gray-600 shrink-0">
                {new Date(entry.time).toLocaleTimeString()}
              </span>
              <span
                className={`shrink-0 w-8 ${levelColors[entry.level] || "text-gray-500"}`}
              >
                {entry.level.toUpperCase().slice(0, 3)}
              </span>
              <span className="text-gray-300">{entry.message}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
