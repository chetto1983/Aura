import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Toaster } from 'sonner';
import { Sidebar } from '@/components/Sidebar';
import { ErrorBoundary } from '@/components/ErrorBoundary';
import { HealthDashboard } from '@/components/HealthDashboard';
import { WikiPanel } from '@/components/WikiPanel';
import { WikiPageView } from '@/components/WikiPageView';
import { SourceInbox } from '@/components/SourceInbox';
import { TasksPanel } from '@/components/TasksPanel';
import { useAppTheme } from '@/hooks/useAppTheme';

const WikiGraphView = lazy(() => import('@/components/WikiGraphView'));

export default function App() {
  useAppTheme(); // applies dark/light/contrast class on <html>
  return (
    <BrowserRouter>
      <div className="flex h-screen w-screen overflow-hidden">
        <Sidebar />
        <main className="flex-1 overflow-auto">
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
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </ErrorBoundary>
        </main>
      </div>
      <Toaster position="bottom-right" />
    </BrowserRouter>
  );
}
