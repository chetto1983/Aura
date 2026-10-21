import { useCallback, useSyncExternalStore } from 'react';

// Below this width the list's fixed columns alone overflow the view, and a name column left to
// take the room left gets none (measured live 2026-10-18: 17 px on a 390 px phone).
const NARROW = '(max-width: 767px)';

function subscribe(media: string, onChange: () => void): () => void {
  if (typeof window.matchMedia !== 'function') return () => undefined;
  const query = window.matchMedia(media);
  query.addEventListener('change', onChange);
  return () => {
    query.removeEventListener('change', onChange);
  };
}

function isWide(media: string): boolean {
  return typeof window.matchMedia !== 'function' || !window.matchMedia(media).matches;
}

/** useWideView reports a view wide enough for the list's name column to take the room left. */
export function useWideView(media = NARROW): boolean {
  const listen = useCallback((change: () => void) => subscribe(media, change), [media]);
  const snapshot = useCallback(() => isWide(media), [media]);
  return useSyncExternalStore(listen, snapshot);
}
