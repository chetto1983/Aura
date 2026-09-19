import { StrictMode, Suspense, lazy, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { exportLog, log, subscribe } from './log.js';
import './spike.css';

// Each lab is its own lazy chunk, so the build reports what each library costs on its own.
const PhotoLab = lazy(() => import('./PhotoLab.jsx'));
const VideoLab = lazy(() => import('./VideoLab.jsx'));

function App() {
  const [tab, setTab] = useState(new URLSearchParams(location.search).get('tab') ?? 'photo');
  const [events, setEvents] = useState([]);
  useEffect(() => subscribe(setEvents), []);
  useEffect(() => log('app', `tab ${tab}`), [tab]);
  return (
    <main>
      <header>
        <strong>Spike 105 · Studio editing</strong>
        <nav>
          <button className={tab === 'photo' ? 'on' : ''} onClick={() => setTab('photo')}>Foto · Filerobot</button>
          <button className={tab === 'video' ? 'on' : ''} onClick={() => setTab('video')}>Video · Mediabunny</button>
        </nav>
        <button onClick={exportLog}>Esporta log ({events.length})</button>
      </header>
      <Suspense fallback={<p>carico…</p>}>{tab === 'photo' ? <PhotoLab /> : <VideoLab />}</Suspense>
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
