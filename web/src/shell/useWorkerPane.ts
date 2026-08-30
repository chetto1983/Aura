import { useCallback, useEffect, useRef, useState } from 'react';
import type { CloseIntent, SurfaceRestore } from './useSurfaceRestore';

export const CHAT_WORKER_PANEL_ID = 'chat-worker';
const WORKER_STATE_KEY = 'aura.shell.worker-pane';
const WORKER_DESKTOP_QUERY = '(min-width: 64rem)';

interface StoredWorkerPane {
  readonly conversationId: string;
  readonly childId: string;
  readonly open: boolean;
}

const CLOSED_WORKER_PANE: StoredWorkerPane = { conversationId: '', childId: '', open: false };

interface WorkerPaneSession {
  readonly activeConversationId: string;
  readonly pane: StoredWorkerPane;
  readonly routeResetCount: number;
}

function readStoredWorkerPane(): StoredWorkerPane {
  try {
    const raw = localStorage.getItem(WORKER_STATE_KEY);
    if (raw === null) return CLOSED_WORKER_PANE;
    const value: unknown = JSON.parse(raw);
    if (
      typeof value !== 'object' ||
      value === null ||
      !('conversationId' in value) ||
      typeof value.conversationId !== 'string' ||
      !('childId' in value) ||
      typeof value.childId !== 'string' ||
      !('open' in value) ||
      typeof value.open !== 'boolean'
    ) {
      return CLOSED_WORKER_PANE;
    }
    return { conversationId: value.conversationId, childId: value.childId, open: value.open };
  } catch {
    return CLOSED_WORKER_PANE;
  }
}

function initialWorkerPaneSession(conversationId: string): WorkerPaneSession {
  const pane = readStoredWorkerPane();
  const belongsElsewhere =
    conversationId.length > 0 && pane.open && pane.conversationId !== conversationId;
  return {
    activeConversationId: conversationId,
    pane: belongsElsewhere ? CLOSED_WORKER_PANE : pane,
    routeResetCount: 0,
  };
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
  conversationId: string,
): WorkerPaneState {
  const isDesktop = useIsWorkerDesktop();
  const { closeOverlay, openOverlay, overlayOpen } = surfaces;
  const [workerSession, setWorkerSession] = useState(() =>
    initialWorkerPaneSession(conversationId),
  );
  let currentSession = workerSession;
  if (workerSession.activeConversationId !== conversationId) {
    const belongsElsewhere =
      conversationId.length > 0 &&
      workerSession.pane.open &&
      workerSession.pane.conversationId !== conversationId;
    currentSession = {
      activeConversationId: conversationId,
      pane: belongsElsewhere ? CLOSED_WORKER_PANE : workerSession.pane,
      routeResetCount: workerSession.routeResetCount + (belongsElsewhere ? 1 : 0),
    };
    setWorkerSession(currentSession);
  }
  const storedPane = currentSession.pane;
  const belongsToConversation =
    conversationId.length > 0 && storedPane.conversationId === conversationId;
  const watchedChildId = belongsToConversation ? storedPane.childId : '';
  const workerOpen = belongsToConversation && storedPane.open && watchedChildId.length > 0;

  useEffect(() => {
    try {
      localStorage.setItem(WORKER_STATE_KEY, JSON.stringify(storedPane));
    } catch {
      // Persistence is best-effort; the current rail state remains authoritative.
    }
  }, [storedPane]);

  const handledRouteReset = useRef(0);
  useEffect(() => {
    if (handledRouteReset.current === currentSession.routeResetCount) return;
    handledRouteReset.current = currentSession.routeResetCount;
    if (!isDesktop && overlayOpen) closeOverlay('explicit');
  }, [closeOverlay, currentSession.routeResetCount, isDesktop, overlayOpen]);

  const openWorker = useCallback(
    (childId: string) => {
      if (childId.length === 0 || conversationId.length === 0) return;
      onBeforeOpen();
      setWorkerSession((current) => ({
        activeConversationId: conversationId,
        pane: { conversationId, childId, open: true },
        routeResetCount: current.routeResetCount,
      }));
      if (!isDesktop) openOverlay();
    },
    [conversationId, isDesktop, onBeforeOpen, openOverlay],
  );

  const closeWorker = useCallback(
    (intent: CloseIntent = 'explicit') => {
      setWorkerSession((current) => ({
        activeConversationId: conversationId,
        pane: CLOSED_WORKER_PANE,
        routeResetCount: current.routeResetCount,
      }));
      if (!isDesktop && overlayOpen) closeOverlay(intent);
    },
    [closeOverlay, conversationId, isDesktop, overlayOpen],
  );

  const workerPanelMounted = isDesktop && workerOpen && watchedChildId.length > 0;
  const panelIds = workerPanelMounted ? [...basePanelIds, CHAT_WORKER_PANEL_ID] : basePanelIds;

  return {
    isDesktop,
    watchedChildId,
    workerActive: isDesktop ? workerOpen : workerOpen && overlayOpen,
    workerPanelMounted,
    panelIds,
    openWorker,
    closeWorker,
  };
}
