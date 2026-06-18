import { useCallback, useMemo, useState, type ReactNode } from 'react';
import type { DisplaySource } from './types';
import { SourceExplorerSheet } from './SourceExplorerSheet';
import { SourceExplorerContext, type SourceExplorerController } from './sourceExplorerControls';

// SourceExplorerProvider — the single shared open-state for the read-only Source
// Explorer (D-13: one sheet, two entry points, one registry). The answer-level
// SourcesButton and the deeply-nested citation chips (CitationBubble → the per-type
// evidence cards → DisplayRouter → the tools.Fallback render-prop boundary) both
// need to open ONE sheet without prop-drilling through assistant-ui's render-prop
// composition. The context (sourceExplorerContext.ts) is the seam: this provider
// owns the sheet state + renders exactly one SourceExplorerSheet; consumers call
// openSources() via useSourceExplorer() (imported from sourceExplorerContext).
//
// The registry the sheet renders is whatever the opener hands it — the SAME
// DisplaySource[] the citations resolve against (the per-turn registry on the
// DisplayPayload). The citation path passes a focusRefId; the button path does not.

interface ExplorerState {
  readonly open: boolean;
  readonly sources: readonly DisplaySource[];
  readonly focusRefId: string | undefined;
}

const CLOSED: ExplorerState = { open: false, sources: [], focusRefId: undefined };

export function SourceExplorerProvider({ children }: { readonly children: ReactNode }) {
  const [state, setState] = useState<ExplorerState>(CLOSED);

  const openSources = useCallback((sources: readonly DisplaySource[], focusRefId?: string) => {
    setState({ open: true, sources, focusRefId });
  }, []);

  const close = useCallback(() => {
    setState((prev) => ({ ...prev, open: false }));
  }, []);

  const controller = useMemo<SourceExplorerController>(() => ({ openSources }), [openSources]);

  return (
    <SourceExplorerContext.Provider value={controller}>
      {children}
      <SourceExplorerSheet
        open={state.open}
        sources={state.sources}
        focusRefId={state.focusRefId}
        onClose={close}
      />
    </SourceExplorerContext.Provider>
  );
}
