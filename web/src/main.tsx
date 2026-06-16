import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { AppShell } from './AppShell';
import { ErrorBoundary } from './ErrorBoundary';
import { LoginPage } from './routes/LoginPage';
import { NotFoundView } from './routes/NotFoundView';
import { queryClient } from './queryClient';
import { applyTheme } from './theme/applyTheme';
import './styles/index.css';

// D-08: theme + density are already on <html> from the index.html pre-paint script;
// applyTheme() re-asserts them from localStorage BEFORE React mounts so there is no
// light-mode flash. Do NOT move this after createRoot, and do NOT edit index.html.
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
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<AppShell />} />
            <Route path="*" element={<NotFoundView />} />
          </Routes>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);
