import { useCallback, useEffect, useState } from 'react';
import type { SurfaceRestore } from './useSurfaceRestore';

// 37B plan 07 — the Artefatti panel state seam, extracted from AppShell.tsx so the shell stays
// under the 600-LOC cap (refactor-on-touch). This owns the desktop-vs-mobile decision, the
// persisted open/closed intent, and the dynamic panelIds (no layout-key bump — RESEARCH
// Pattern 1). The presentational pieces live in ./ArtifactsShell.

export const CHAT_ARTIFACTS_PANEL_ID = 'chat-artifacts';
// D-03: the open/closed intent is persisted so the panel survives reload.
const ARTIFACTS_OPEN_KEY = 'aura.shell.artifacts-open';
// The `lg` breakpoint (64rem) mirrors the CSS container-query at which the chat rail collapses;
// at/above it the panel is a ResizablePanel, below it a right Drawer (D-01/D-04).
const ARTIFACTS_DESKTOP_QUERY = '(min-width: 64rem)';

function readArtifactsOpen(): boolean {
  try {
    return localStorage.getItem(ARTIFACTS_OPEN_KEY) === '1';
  } catch {
    return false;
  }
}

// Viewport gate for the desktop-vs-drawer branch. Unlike the nav rail (pure CSS container query),
// the artifacts panel enters/leaves the ResizablePanelGroup dynamically, so its DOM membership
// must be decided in JS — a CSS-hidden panel would still claim group width.
function useIsArtifactsDesktop(): boolean {
  const [isDesktop, setIsDesktop] = useState<boolean>(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true;
    return window.matchMedia(ARTIFACTS_DESKTOP_QUERY).matches;
  });
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const mql = window.matchMedia(ARTIFACTS_DESKTOP_QUERY);
    const onChange = () => {
      setIsDesktop(mql.matches);
    };
    onChange();
    mql.addEventListener('change', onChange);
    return () => {
      mql.removeEventListener('change', onChange);
    };
  }, []);
  return isDesktop;
}

export interface ArtifactsPanelState {
  readonly isDesktop: boolean;
  /** Whether the panel is currently visible on the live surface (desktop panel or mobile drawer). */
  readonly artifactsActive: boolean;
  /** Whether the desktop ResizablePanel is mounted (drives dynamic panelIds membership). */
  readonly artifactsPanelMounted: boolean;
  /** The panel-id set for useDefaultLayout — the third id is added only when mounted. */
  readonly panelIds: readonly string[];
  /** Show the panel on the live surface (used by the one-time auto-open). */
  readonly openArtifacts: () => void;
  /** Flip the panel on the live surface (the header toggle). */
  readonly toggleArtifacts: () => void;
  /** Collapse the desktop panel (its internal close control). */
  readonly closeDesktopPanel: () => void;
  /** Close whichever Artifacts surface is live and release the shared mobile overlay. */
  readonly closePanel: (intent?: 'explicit' | 'swipe') => void;
}

// The panel lives in two surfaces (D-01/D-04): a persisted ResizablePanel on desktop, the shared
// "one heavy overlay" right Drawer on mobile. The toggle + auto-open route to whichever is live so
// a single intent ("show me the artefatti") behaves correctly at both widths.
export function useArtifactsPanel(
  surfaces: SurfaceRestore,
  basePanelIds: readonly string[],
  onBeforeOpen: () => void = () => undefined,
): ArtifactsPanelState {
  const isDesktop = useIsArtifactsDesktop();
  const [artifactsOpen, setArtifactsOpen] = useState<boolean>(readArtifactsOpen);

  useEffect(() => {
    try {
      localStorage.setItem(ARTIFACTS_OPEN_KEY, artifactsOpen ? '1' : '0');
    } catch {
      // Persistence is best-effort; the visible toggle state still holds.
    }
  }, [artifactsOpen]);

  const openArtifacts = useCallback(() => {
    onBeforeOpen();
    setArtifactsOpen(true);
    if (isDesktop) {
      return;
    } else {
      surfaces.openOverlay();
    }
  }, [isDesktop, onBeforeOpen, surfaces]);

  const toggleArtifacts = useCallback(() => {
    if (isDesktop) {
      if (artifactsOpen) {
        setArtifactsOpen(false);
      } else {
        onBeforeOpen();
        setArtifactsOpen(true);
      }
    } else if (artifactsOpen && surfaces.overlayOpen) {
      setArtifactsOpen(false);
      surfaces.closeOverlay('explicit');
    } else {
      onBeforeOpen();
      setArtifactsOpen(true);
      surfaces.openOverlay();
    }
  }, [artifactsOpen, isDesktop, onBeforeOpen, surfaces]);

  const closeDesktopPanel = useCallback(() => {
    setArtifactsOpen(false);
  }, []);

  const closePanel = useCallback(
    (intent: 'explicit' | 'swipe' = 'explicit') => {
      setArtifactsOpen(false);
      if (!isDesktop && surfaces.overlayOpen) surfaces.closeOverlay(intent);
    },
    [isDesktop, surfaces],
  );

  const artifactsPanelMounted = isDesktop && artifactsOpen;
  const panelIds = artifactsPanelMounted
    ? [...basePanelIds, CHAT_ARTIFACTS_PANEL_ID]
    : basePanelIds;

  return {
    isDesktop,
    artifactsActive: isDesktop ? artifactsOpen : artifactsOpen && surfaces.overlayOpen,
    artifactsPanelMounted,
    panelIds,
    openArtifacts,
    toggleArtifacts,
    closeDesktopPanel,
    closePanel,
  };
}
