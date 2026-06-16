import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { AppShell } from './AppShell';
import { ErrorBoundary } from './ErrorBoundary';
import { applyTheme } from './theme/applyTheme';
import './styles/index.css';

applyTheme();

const container = document.getElementById('root');
if (!container) {
  throw new Error('Aura: #root mount point missing from index.html');
}

createRoot(container).render(
  <StrictMode>
    <ErrorBoundary>
      <AppShell />
    </ErrorBoundary>
  </StrictMode>,
);
