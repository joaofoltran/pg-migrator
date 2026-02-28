import { Settings as SettingsIcon } from "lucide-react";

export function SettingsPage() {
  return (
    <div className="space-y-6 max-w-2xl">
      <div>
        <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>Settings</h2>
        <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
          Daemon configuration and preferences
        </p>
      </div>

      <div className="rounded-lg border p-5 space-y-4"
        style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <div className="flex items-center gap-3">
          <SettingsIcon className="w-5 h-5" style={{ color: "var(--color-text-muted)" }} />
          <h3 className="text-sm font-medium" style={{ color: "var(--color-text)" }}>General</h3>
        </div>

        <div className="space-y-3 text-sm">
          <div className="flex items-center justify-between py-2 border-b" style={{ borderColor: "var(--color-border)" }}>
            <span style={{ color: "var(--color-text-secondary)" }}>Daemon port</span>
            <span className="font-mono" style={{ color: "var(--color-text)" }}>7654</span>
          </div>
          <div className="flex items-center justify-between py-2 border-b" style={{ borderColor: "var(--color-border)" }}>
            <span style={{ color: "var(--color-text-secondary)" }}>Data directory</span>
            <span className="font-mono" style={{ color: "var(--color-text)" }}>~/.migrator</span>
          </div>
          <div className="flex items-center justify-between py-2">
            <span style={{ color: "var(--color-text-secondary)" }}>Version</span>
            <span className="font-mono" style={{ color: "var(--color-text)" }}>0.1.0-dev</span>
          </div>
        </div>
      </div>
    </div>
  );
}
