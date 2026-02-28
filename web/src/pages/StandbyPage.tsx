import { Server } from "lucide-react";

export function StandbyPage() {
  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center space-y-4 max-w-md">
        <div className="w-16 h-16 rounded-2xl flex items-center justify-center mx-auto"
          style={{ backgroundColor: "var(--color-surface)" }}>
          <Server className="w-8 h-8" style={{ color: "var(--color-text-muted)" }} />
        </div>
        <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>Standby Management</h2>
        <p className="text-sm leading-relaxed" style={{ color: "var(--color-text-muted)" }}>
          Monitor and manage PostgreSQL replicas with automatic failover, health checks, and promotion workflows.
        </p>
        <span className="inline-block text-xs px-3 py-1.5 rounded-full font-medium"
          style={{ backgroundColor: "var(--color-surface)", color: "var(--color-text-muted)" }}>
          Coming soon
        </span>
      </div>
    </div>
  );
}
