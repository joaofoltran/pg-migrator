import type { Snapshot } from "../types/metrics";
import { PhaseIndicator } from "./PhaseIndicator";
import { ProgressBar } from "./ProgressBar";
import { ThroughputCard } from "./ThroughputCard";
import { TableProgress } from "./TableProgress";
import { LagChart } from "./LagChart";
import { LogViewer } from "./LogViewer";

interface DashboardProps {
  snapshot: Snapshot;
  connected: boolean;
  history: Snapshot[];
}

export function Dashboard({ snapshot, connected, history }: DashboardProps) {
  return (
    <div className="min-h-screen bg-gray-950 text-gray-100">
      {/* Header */}
      <header className="bg-gray-900 border-b border-gray-800 px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold text-purple-400">pgmigrator</h1>
            <span
              className={`w-2 h-2 rounded-full ${connected ? "bg-green-500" : "bg-red-500"}`}
            />
            <span className="text-xs text-gray-500">
              {connected ? "connected" : "disconnected"}
            </span>
          </div>
          <PhaseIndicator snapshot={snapshot} />
        </div>
      </header>

      {/* Content */}
      <main className="max-w-7xl mx-auto px-6 py-6 space-y-6">
        {/* Metrics cards */}
        <ThroughputCard snapshot={snapshot} />

        {/* Progress bar */}
        <ProgressBar snapshot={snapshot} />

        {/* Two-column layout for chart + logs */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <LagChart history={history} />
          <LogViewer />
        </div>

        {/* Table progress */}
        <div>
          <h2 className="text-sm text-gray-400 mb-3">Table Progress</h2>
          <TableProgress tables={snapshot.tables} />
        </div>
      </main>

      {/* Footer */}
      <footer className="border-t border-gray-800 px-6 py-3 text-xs text-gray-600 text-center">
        pgmigrator — PostgreSQL online migration tool
      </footer>
    </div>
  );
}
