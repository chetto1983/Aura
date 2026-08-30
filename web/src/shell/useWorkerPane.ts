import { useCallback, useEffect, useState } from 'react';
import type { CloseIntent, SurfaceRestore } from './useSurfaceRestore';

export const CHAT_WORKER_PANEL_ID = 'chat-worker';
const WORKER_OPEN_KEY = 'aura.shell.worker-open';
const WORKER_CHILD_KEY = 'aura.shell.worker-child';
const WORKER_DESKTOP_QUERY = '(min-width: 64rem)';

function readStored(key: string): string {
  try {
    return localStorage.getItem(key) ?? '';
  } catch {
    return '';
  }
}

function useIsWorkerDesktop(): boolean {
  const [isDesktop, setIsDesktop] = useState(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true;
    return window.matchMedia(WORKER_DESKTOP_QUERY).matches;
  });

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const media = window.matchMedia(WORKER_DESKTOP_QUERY);
    const onChange = () => {
      setIsDesktop(media.matches);
    };
    onChange();
    media.addEventListener('change', onChange);
    return () => {
      media.removeEventListener('change', onChange);
    };
  }, []);
  return isDesktop;
}

export interface WorkerPaneState {
  readonly isDesktop: boolean;
  readonly watchedChildId: string;
  readonly workerActive: boolean;
  readonly workerPanelMounted: boolean;
  readonly panelIds: readonly string[];
  readonly openWorker: (childId: string) => void;
  readonly closeWorker: (intent?: CloseIntent) => void;
}

export function useWorkerPane(
  surfaces: SurfaceRestore,
  basePanelIds: readonly string[],
  onBeforeOpen: () => void,
): WorkerPaneState {
  const isDesktop = useIsWorkerDesktop();
  const [watchedChildId, setWatchedChildId] = useState(() => readStored(WORKER_CHILD_KEY));
  const [workerOpen, setWorkerOpen] = useState(
    () => readStored(WORKER_OPEN_KEY) === '1' && readStored(WORKER_CHILD_KEY).length > 0,
  );

  useEffect(() => {
    try {
      localStorage.setItem(WORKER_OPEN_KEY, workerOpen ? '1' : '0');
      if (watchedChildId.length > 0) {
        localStorage.setItem(WORKER_CHILD_KEY, watchedChildId);
      } else {
        localStorage.removeItem(WORKER_CHILD_KEY);
      }
    } catch {
      // Persistence is best-effort; the current rail state remains authoritative.
    }
  }, [watchedChildId, workerOpen]);

  const openWorker = useCallback(
    (childId: string) => {
      if (childId.length === 0) return;
      onBeforeOpen();
      setWatchedChildId(childId);
      setWorkerOpen(true);
      if (!isDesktop) surfaces.openOverlay();
    },
    [isDesktop, onBeforeOpen, surfaces],
  );

  const closeWorker = useCallback(
    (intent: CloseIntent = 'explicit') => {
      setWorkerOpen(false);
      setWatchedChildId('');
      if (!isDesktop && surfaces.overlayOpen) surfaces.closeOverlay(intent);
    },
    [isDesktop, surfaces],
  );

  const workerPanelMounted = isDesktop && workerOpen && watchedChildId.length > 0;
  const panelIds = workerPanelMounted ? [...basePanelIds, CHAT_WORKER_PANEL_ID] : basePanelIds;

  return {
    isDesktop,
    watchedChildId,
    workerActive: isDesktop ? workerOpen : workerOpen && surfaces.overlayOpen,
    workerPanelMounted,
    panelIds,
    openWorker,
    closeWorker,
  };
}
