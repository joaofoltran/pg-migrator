import { useMetrics } from "../hooks/useMetrics";
import { PhaseHeader } from "../components/migration/PhaseHeader";
import { MetricCards } from "../components/migration/MetricCards";
import { OverallProgress } from "../components/migration/OverallProgress";
import { LagChart } from "../components/migration/LagChart";
import { LogViewer } from "../components/migration/LogViewer";
import { TableList } from "../components/migration/TableList";
import { JobControls } from "../components/migration/JobControls";
import { Loader2 } from "lucide-react";

export function MigrationPage() {
  const { snapshot, connected, history } = useMetrics();

  if (!snapshot) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center space-y-3">
          <Loader2 className="w-8 h-8 animate-spin mx-auto" style={{ color: "var(--color-accent)" }} />
          <p style={{ color: "var(--color-text-muted)" }}>Connecting to daemon...</p>
        </div>
      </div>
    );
  }

  const idle = snapshot.phase === "idle" || snapshot.phase === "";

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
    </div>
  );
}
