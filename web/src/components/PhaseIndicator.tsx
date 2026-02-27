import type { Snapshot } from "../types/metrics";

const phaseColors: Record<string, string> = {
  idle: "bg-gray-600",
  connecting: "bg-yellow-500",
  schema: "bg-blue-500",
  copy: "bg-purple-500",
  streaming: "bg-green-500",
  switchover: "bg-orange-500",
  "switchover-complete": "bg-green-600",
  done: "bg-green-700",
};

function formatElapsed(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}h ${m}m ${s}s`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

export function PhaseIndicator({ snapshot }: { snapshot: Snapshot }) {
  const color = phaseColors[snapshot.phase] || "bg-gray-500";

  return (
    <div className="flex items-center gap-4">
      <span
        className={`${color} text-white px-3 py-1 rounded-full text-sm font-bold uppercase`}
      >
        {snapshot.phase}
      </span>
      <span className="text-gray-400">
        Elapsed: {formatElapsed(snapshot.elapsed_sec)}
      </span>
      {snapshot.error_count > 0 && (
        <span className="text-red-400 text-sm">
          {snapshot.error_count} error{snapshot.error_count !== 1 ? "s" : ""}
        </span>
      )}
    </div>
  );
}
