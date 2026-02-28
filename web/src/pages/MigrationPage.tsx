import { useMetrics } from "../hooks/useMetrics";
import { PhaseHeader } from "../components/migration/PhaseHeader";
import { MetricCards } from "../components/migration/MetricCards";
import { OverallProgress } from "../components/migration/OverallProgress";
import { LagChart } from "../components/migration/LagChart";
import { LogViewer } from "../components/migration/LogViewer";
import { TableList } from "../components/migration/TableList";
import { JobControls } from "../components/migration/JobControls";
import { WifiOff } from "lucide-react";

export function MigrationPage() {
  const { snapshot, connected, history } = useMetrics();

  const idle = !snapshot || snapshot.phase === "idle" || snapshot.phase === "";

  return (
    <div className="space-y-6 max-w-7xl">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>Migration</h2>
          <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
            Online database migration & CDC streaming
          </p>
        </div>
        <JobControls idle={idle} connected={connected} />
      </div>

      {!connected && !snapshot ? (
        <div className="rounded-lg border p-8 text-center"
          style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <WifiOff className="w-8 h-8 mx-auto mb-3" style={{ color: "var(--color-text-muted)" }} />
          <p className="font-medium" style={{ color: "var(--color-text)" }}>Daemon not reachable</p>
          <p className="text-sm mt-1" style={{ color: "var(--color-text-muted)" }}>
            Start the daemon with <code className="px-1.5 py-0.5 rounded text-xs font-mono"
            style={{ backgroundColor: "var(--color-bg)", color: "var(--color-accent)" }}>pgmanager daemon start</code> to manage migrations.
          </p>
        </div>
      ) : snapshot ? (
        <>
          <PhaseHeader snapshot={snapshot} connected={connected} />
          {!idle && (
            <>
              <MetricCards snapshot={snapshot} />
              <OverallProgress snapshot={snapshot} />
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                <LagChart history={history} />
                <LogViewer />
              </div>
              <TableList tables={snapshot.tables} />
            </>
          )}
        </>
      ) : null}
    </div>
  );
}
