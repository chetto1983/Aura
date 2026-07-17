import { useCallback, useState } from 'react';

// 37F plan 14 — the share modal state seam, extracted from AppShell.tsx so the shell stays under
// the 600-LOC cap (refactor-on-touch), mirroring useArtifactsPanel.ts's stated reason for
// existing (:4-7). The presentational piece lives in ./ShareShell.
//
// Deliberately NOT copied from useArtifactsPanel: its browser-storage persistence block and the
// desktop/mobile split. A share modal is not a persisted panel — persisting the open flag would
// resurrect a stale "share modal open" across reloads — and a modal has no desktop/mobile shape
// to split on. Kept to ~30 LOC by design (37F-PATTERNS.md §useSharePanel.ts): if this ever
// approaches useArtifactsPanel's 117 lines, the wrong analog was followed.

export type ShareModalState = 'closed' | 'open';

export interface SharePanelState {
  /** 'closed' initially and never persisted — a reload always starts closed. */
  readonly shareModalState: ShareModalState;
  /** Opens the share modal for the active thread. */
  readonly openShare: () => void;
  /** Closes the share modal. */
  readonly closeShare: () => void;
}

export function useSharePanel(): SharePanelState {
  const [shareModalState, setShareModalState] = useState<ShareModalState>('closed');

  const openShare = useCallback(() => {
    setShareModalState('open');
  }, []);

  const closeShare = useCallback(() => {
    setShareModalState('closed');
  }, []);

  return { shareModalState, openShare, closeShare };
}
