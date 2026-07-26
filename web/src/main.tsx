/* eslint-disable react-refresh/only-export-components */
import { StrictMode, Suspense, lazy } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Route, Routes } from 'react-router';
import { RouteSkeletonFallback } from './components/skeleton';
import { ErrorBoundary } from './ErrorBoundary';
import { queryClient } from './queryClient';
import { installMutationIdempotency } from './api/idempotency';
import { applyTheme } from './theme/applyTheme';
import './i18n/i18n';
import './styles/index.css';

installMutationIdempotency();

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

// D-08: theme + density are already on <html> from the index.html pre-paint script.
// Re-assert them from localStorage BEFORE React mounts so there is no theme flash.
applyTheme();

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
        <BrowserRouter>
          <Suspense fallback={<RouteSkeletonFallback />}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/" element={<AppShell />} />
              {/* Deep link to a conversation at a search match (D-08); AppShell
                  reads :id into the active thread. */}
              <Route path="/c/:id" element={<AppShell />} />
              {/* WEBSHARE-02/03 (37F-16): /s/{token} renders WITHOUT a session — the
                  server admits this prefix to the SPA shell unauthenticated
                  (isPublicShareRoute, plan 37F-12) and the 404 from GET
                  /s/{token}/data is the real gate, not this route. /shared/{id} is
                  deliberately OUTSIDE the /s/ prefix and stays behind RequireAuth;
                  its gate is the 401/404 from GET /api/shares/{id}/data. Neither
                  route hides anything client-side — same rule as this file's
                  header comment above. */}
              <Route path="/s/:token" element={<SharePage tier="public" />} />
              <Route path="/shared/:id" element={<SharePage tier="internal" />} />
              <Route path="*" element={<NotFoundView />} />
            </Routes>
          </Suspense>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);
