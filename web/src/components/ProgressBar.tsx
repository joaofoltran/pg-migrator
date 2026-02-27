import type { Snapshot } from "../types/metrics";

export function ProgressBar({ snapshot }: { snapshot: Snapshot }) {
  const total = snapshot.tables_total;
  const copied = snapshot.tables_copied;
  const pct = total > 0 ? (copied / total) * 100 : 0;

  return (
    <div className="bg-gray-800 rounded-lg p-4">
      <div className="flex justify-between text-sm text-gray-400 mb-2">
        <span>Overall Progress</span>
        <span>
          {copied}/{total} tables ({pct.toFixed(1)}%)
        </span>
      </div>
      <div className="w-full bg-gray-700 rounded-full h-3">
        <div
          className="bg-purple-500 h-3 rounded-full transition-all duration-300"
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}
