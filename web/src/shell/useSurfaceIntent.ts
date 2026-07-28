import { useCallback, useState } from 'react';
import { MODES, type SurfaceIntent } from './modes';

const SURFACE_INTENT_KEY = 'aura.shell.surface';

function readSurface(): SurfaceIntent {
  try {
    const value = localStorage.getItem(SURFACE_INTENT_KEY);
    return MODES.includes(value as SurfaceIntent) ? (value as SurfaceIntent) : 'chat';
  } catch {
    return 'chat';
  }
}

export function useSurfaceIntent() {
  const [surface, setSurfaceState] = useState<SurfaceIntent>(readSurface);

  const setSurface = useCallback((next: SurfaceIntent) => {
    setSurfaceState(next);
    try {
      localStorage.setItem(SURFACE_INTENT_KEY, next);
    } catch {
      // Storage is optional; the visible state still updates.
    }
  }, []);

  return { surface, setSurface };
}
