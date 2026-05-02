import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { Toaster } from 'sonner';
import { Shell } from '@/components/Shell';
import { ErrorBoundary } from '@/components/ErrorBoundary';
import { HealthDashboard } from '@/components/HealthDashboard';
import { Login } from '@/components/Login';
import { ConfirmHost } from '@/components/common/ConfirmModal';
import { getToken } from '@/lib/auth';
import { useAppTheme } from '@/hooks/useAppTheme';

// Code-splitting: only HealthDashboard + Shell + Login are eagerly
// loaded (~core UX). Every other panel lazy-imports so a fresh dashboard
// open doesn't ship the union of all panel JS. Pre-split bundle was
// 580 KB; this drops the home-route entry to roughly the React +
// shell + dashboard skeleton.
const WikiPanel = lazy(() => import('@/components/WikiPanel').then((m) => ({ default: m.WikiPanel })));
const WikiPageView = lazy(() => import('@/components/WikiPageView').then((m) => ({ default: m.WikiPageView })));
const WikiGraphView = lazy(() => import('@/components/WikiGraphView'));
const SourceInbox = lazy(() => import('@/components/SourceInbox').then((m) => ({ default: m.SourceInbox })));
const TasksPanel = lazy(() => import('@/components/TasksPanel').then((m) => ({ default: m.TasksPanel })));
const SkillsPanel = lazy(() => import('@/components/SkillsPanel').then((m) => ({ default: m.SkillsPanel })));
const MCPPanel = lazy(() => import('@/components/MCPPanel').then((m) => ({ default: m.MCPPanel })));
const PendingUsersPanel = lazy(() => import('@/components/PendingUsersPanel').then((m) => ({ default: m.PendingUsersPanel })));
const ConversationsPanel = lazy(() => import('@/components/ConversationsPanel').then((m) => ({ default: m.ConversationsPanel })));
const SummariesPanel = lazy(() => import('@/components/SummariesPanel').then((m) => ({ default: m.SummariesPanel })));
const MaintenancePanel = lazy(() => import('@/components/MaintenancePanel').then((m) => ({ default: m.MaintenancePanel })));
const SettingsPanel = lazy(() => import('@/components/SettingsPanel').then((m) => ({ default: m.SettingsPanel })));

// RequireAuth gates everything except /login. If no token is present we
// redirect — api.ts also handles 401 redirects, but this gate avoids an
// initial flash of "Loading…" / Error state while the first request
// fires. The real validity check still happens on the first API call.
function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  if (!getToken()) {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }
  return <>{children}</>;
}

function PanelLoading() {
  return <div className="p-6 text-sm text-muted-foreground">Loading…</div>;
}

export default function App() {
  useAppTheme(); // applies dark/light/contrast class on <html>
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          path="*"
          element={
            <RequireAuth>
              <Shell>
                <ErrorBoundary>
                  <Suspense fallback={<PanelLoading />}>
                    <Routes>
                      <Route path="/" element={<HealthDashboard />} />
                      <Route path="/wiki" element={<WikiPanel />} />
                      <Route path="/wiki/:slug" element={<WikiPageView />} />
                      <Route path="/graph" element={<WikiGraphView />} />
                      <Route path="/sources" element={<SourceInbox />} />
                      <Route path="/tasks" element={<TasksPanel />} />
                      <Route path="/skills" element={<SkillsPanel />} />
                      <Route path="/mcp" element={<MCPPanel />} />
                      <Route path="/pending" element={<PendingUsersPanel />} />
                      <Route path="/conversations" element={<ConversationsPanel />} />
                      <Route path="/summaries" element={<SummariesPanel />} />
                      <Route path="/maintenance" element={<MaintenancePanel />} />
                      <Route path="/settings" element={<SettingsPanel />} />
                      <Route path="*" element={<Navigate to="/" replace />} />
                    </Routes>
                  </Suspense>
                </ErrorBoundary>
              </Shell>
            </RequireAuth>
          }
        />
      </Routes>
      <Toaster position="bottom-right" />
      <ConfirmHost />
    </BrowserRouter>
  );
}
