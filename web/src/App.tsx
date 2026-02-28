import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Layout } from "./components/layout/Layout";
import { ClustersPage } from "./pages/ClustersPage";
import { MigrationPage } from "./pages/MigrationPage";
import { BackupPage } from "./pages/BackupPage";
import { StandbyPage } from "./pages/StandbyPage";
import { SettingsPage } from "./pages/SettingsPage";

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Navigate to="/clusters" replace />} />
          <Route path="/clusters" element={<ClustersPage />} />
          <Route path="/migration" element={<MigrationPage />} />
          <Route path="/backup" element={<BackupPage />} />
          <Route path="/standby" element={<StandbyPage />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
