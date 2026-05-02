import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { Toaster } from 'sonner';
import { Shell } from '@/components/Shell';
import { ErrorBoundary } from '@/components/ErrorBoundary';
import { HealthDashboard } from '@/components/HealthDashboard';
import { WikiPanel } from '@/components/WikiPanel';
import { WikiPageView } from '@/components/WikiPageView';
import { SourceInbox } from '@/components/SourceInbox';
import { TasksPanel } from '@/components/TasksPanel';
import { SkillsPanel } from '@/components/SkillsPanel';
import { MCPPanel } from '@/components/MCPPanel';
import { PendingUsersPanel } from '@/components/PendingUsersPanel';
import { ConversationsPanel } from '@/components/ConversationsPanel';
import { Login } from '@/components/Login';
import { getToken } from '@/lib/auth';
import { useAppTheme } from '@/hooks/useAppTheme';

const WikiGraphView = lazy(() => import('@/components/WikiGraphView'));

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
                  <Routes>
                    <Route path="/" element={<HealthDashboard />} />
                    <Route path="/wiki" element={<WikiPanel />} />
                    <Route path="/wiki/:slug" element={<WikiPageView />} />
                    <Route path="/graph" element={
                      <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">Loading graph…</div>}>
                        <WikiGraphView />
                      </Suspense>
                    } />
                    <Route path="/sources" element={<SourceInbox />} />
                    <Route path="/tasks" element={<TasksPanel />} />
                    <Route path="/skills" element={<SkillsPanel />} />
                    <Route path="/mcp" element={<MCPPanel />} />
                    <Route path="/pending" element={<PendingUsersPanel />} />
                    <Route path="/conversations" element={<ConversationsPanel />} />
                    <Route path="*" element={<Navigate to="/" replace />} />
                  </Routes>
                </ErrorBoundary>
              </Shell>
            </RequireAuth>
          }
        />
      </Routes>
      <Toaster position="bottom-right" />
    </BrowserRouter>
  );
}
