import { createContext, useContext } from 'react';
import type { DisplaySource } from './types';

// sourceExplorerContext — the context object + the consumer hook for the shared
// read-only Source Explorer (D-13). Kept in a NON-component module so the provider
// .tsx can stay component-only (react-refresh/only-export-components).

export interface SourceExplorerController {
  /** Open the shared sheet over `sources`, optionally focused on a specific refId. */
  readonly openSources: (sources: readonly DisplaySource[], focusRefId?: string) => void;
}

const NOOP_CONTROLLER: SourceExplorerController = { openSources: () => undefined };

export const SourceExplorerContext = createContext<SourceExplorerController | null>(null);

/** Open the shared Source Explorer; a no-op controller when no provider is mounted. */
export function useSourceExplorer(): SourceExplorerController {
  return useContext(SourceExplorerContext) ?? NOOP_CONTROLLER;
}
