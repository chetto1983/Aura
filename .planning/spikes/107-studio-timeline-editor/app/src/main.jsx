import { StrictMode, Suspense, lazy, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { exportLog, log, subscribe } from './log.js';
import './spike.css';

// Each lab is its own lazy chunk, and vite.config.js splits every npm package into its own chunk,
// so the build reports what each library costs on its own.
const LABS = {
  compose: ['Composizione · VideoFlow', lazy(() => import('./ComposeLab.jsx'))],
  dnd: ['Timeline · dnd-timeline', lazy(() => import('./DndTimelineLab.jsx'))],
  xz: ['Timeline · react-timeline-editor', lazy(() => import('./XzTimelineLab.jsx'))],
  history: ['Undo/redo', lazy(() => import('./HistoryLab.jsx'))],
  speed: ['Velocità · pitch', lazy(() => import('./SpeedLab.jsx'))],
};

function App() {
  const [tab, setTab] = useState(new URLSearchParams(location.search).get('tab') ?? 'compose');
  const [events, setEvents] = useState([]);
  useEffect(() => subscribe(setEvents), []);
  useEffect(() => log('app', `tab ${tab}`), [tab]);
  const Lab = LABS[tab][1];
  return (
    <main>
      <header>
        <strong>Spike 107 · Studio timeline</strong>
        <nav>
          {Object.entries(LABS).map(([key, [label]]) => (
            <button key={key} className={tab === key ? 'on' : ''} onClick={() => setTab(key)}>{label}</button>
          ))}
        </nav>
        <button onClick={exportLog}>Esporta log ({events.length})</button>
      </header>
      <Suspense fallback={<p>carico…</p>}><Lab /></Suspense>
      <details>
        <summary>Log</summary>
        <pre>{events.map((e) => `${e.at} [${e.category}] ${e.message}`).join('\n')}</pre>
      </details>
    </main>
  );
}

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
