import { useState } from "react";
import { Play, Square, Loader2 } from "lucide-react";
import { submitClone, submitFollow, stopJob } from "../../api/client";

interface Props {
  idle: boolean;
  connected: boolean;
}

export function JobControls({ idle, connected }: Props) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showMenu, setShowMenu] = useState(false);

  async function handleStart(mode: "clone" | "follow") {
    setLoading(true);
    setError(null);
    setShowMenu(false);
    try {
      if (mode === "clone") {
        await submitClone();
      } else {
        await submitFollow();
      }
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to start job");
    } finally {
      setLoading(false);
    }
  }

  async function handleStop() {
    setLoading(true);
    setError(null);
    try {
      await stopJob();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to stop job");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="relative flex items-center gap-3">
      {error && (
        <span className="text-xs text-red-400">{error}</span>
      )}

      {idle ? (
        <div className="relative">
          <button
            disabled={!connected || loading}
            onClick={() => setShowMenu((v) => !v)}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
            style={{
              backgroundColor: "var(--color-accent)",
              color: "#fff",
            }}
          >
            {loading ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Play className="w-4 h-4" />
            )}
            Start
          </button>

          {showMenu && (
            <>
              <div className="fixed inset-0 z-10" onClick={() => setShowMenu(false)} />
              <div
                className="absolute right-0 top-full mt-1 z-20 rounded-lg border py-1 min-w-[160px]"
                style={{
                  backgroundColor: "var(--color-surface)",
                  borderColor: "var(--color-border)",
                }}
              >
                <button
                  className="w-full text-left px-4 py-2 text-sm transition-colors hover:bg-white/5"
                  style={{ color: "var(--color-text)" }}
                  onClick={() => handleStart("clone")}
                >
                  Clone
                  <span className="block text-xs" style={{ color: "var(--color-text-muted)" }}>
                    Full copy + streaming
                  </span>
                </button>
                <button
                  className="w-full text-left px-4 py-2 text-sm transition-colors hover:bg-white/5"
                  style={{ color: "var(--color-text)" }}
                  onClick={() => handleStart("follow")}
                >
                  Follow
                  <span className="block text-xs" style={{ color: "var(--color-text-muted)" }}>
                    CDC streaming only
                  </span>
                </button>
              </div>
            </>
          )}
        </div>
      ) : (
        <button
          disabled={!connected || loading}
          onClick={handleStop}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
          style={{
            backgroundColor: "#dc2626",
            color: "#fff",
          }}
        >
          {loading ? (
            <Loader2 className="w-4 h-4 animate-spin" />
          ) : (
            <Square className="w-4 h-4" />
          )}
          Stop
        </button>
      )}
    </div>
  );
}
