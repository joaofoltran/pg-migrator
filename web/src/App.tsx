import { useMetrics } from "./hooks/useMetrics";
import { Dashboard } from "./components/Dashboard";

export function App() {
  const { snapshot, connected, history } = useMetrics();

  if (!snapshot) {
    return (
      <div className="min-h-screen bg-gray-950 flex items-center justify-center">
        <div className="text-center">
          <h1 className="text-2xl font-bold text-purple-400 mb-2">
            pgmigrator
          </h1>
          <p className="text-gray-500">Connecting to migration server...</p>
          <div className="mt-4 animate-pulse">
            <div className="w-8 h-8 border-2 border-purple-500 border-t-transparent rounded-full animate-spin mx-auto" />
          </div>
        </div>
      </div>
    );
  }

  return (
    <Dashboard snapshot={snapshot} connected={connected} history={history} />
  );
}
