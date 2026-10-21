/* eslint-disable react-refresh/only-export-components */
import { StrictMode, Suspense, lazy } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { createBrowserRouter, RouterProvider } from 'react-router';
import { RouteSkeletonFallback } from './components/skeleton';
import { ErrorBoundary } from './ErrorBoundary';
import { queryClient } from './queryClient';
import { installMutationIdempotency } from './api/idempotency';
import { installSessionExpiryRedirect } from './api/sessionExpiry';
import { applyTheme } from './theme/applyTheme';
import './i18n/i18n';
import './styles/index.css';

installMutationIdempotency();
installSessionExpiryRedirect();

const AppShell = lazy(() => import('./AppShell').then((mod) => ({ default: mod.AppShell })));
const LoginPage = lazy(() =>
  import('./routes/LoginPage').then((mod) => ({ default: mod.LoginPage })),
);
const NotFoundView = lazy(() =>
  import('./routes/NotFoundView').then((mod) => ({ default: mod.NotFoundView })),
);
const SharePage = lazy(() =>
  import('./routes/SharePage').then((mod) => ({ default: mod.SharePage })),
);
const BrowserLivePage = lazy(() =>
  import('./routes/BrowserLivePage').then((mod) => ({ default: mod.BrowserLivePage })),
);

// D-08: theme + density are already on <html> from the index.html pre-paint script.
// Re-assert them from localStorage BEFORE React mounts so there is no theme flash.
applyTheme();

// Server authentication remains the boundary; the data router enables document leave guards.
const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  { path: '/', element: <AppShell /> },
  { path: '/c/:id', element: <AppShell /> },
  { path: '/documents/*', element: <AppShell /> },
  { path: '/s/:token', element: <SharePage tier="public" /> },
  { path: '/shared/:id', element: <SharePage tier="internal" /> },
  { path: '/browser/:session', element: <BrowserLivePage /> },
  { path: '*', element: <NotFoundView /> },
]);

const container = document.getElementById('root');
if (!container) {
  throw new Error('Aura: #root mount point missing from index.html');
}

// The router provides client navigation + a client-side 404 (NotFoundView) only.
// It is NOT an auth boundary — the server's RequireAuth (Plan 24-03) is the real
// gate; we never hide a route purely client-side (T-24-21).
createRoot(container).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <Suspense fallback={<RouteSkeletonFallback />}>
          <RouterProvider router={router} />
        </Suspense>
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);
