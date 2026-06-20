export const MODES = ['chat', 'tree', 'graph', 'governance', 'displays', 'settings'] as const;

export type SurfaceIntent = (typeof MODES)[number];

// 'graph' is live as of Phase 27 — the Frame-06 Graph Explorer workspace (D-11).
// 'governance' is live as of Phase 28 — the read-only MCP/Skills/Scheduler boards (D-01).
// Both are reachable surfaces beside chat; 'tree'/'displays'/'settings' remain placeholders.
export const LIVE_MODES = ['chat', 'graph', 'governance'] as const satisfies readonly SurfaceIntent[];

export function isLiveSurfaceIntent(mode: SurfaceIntent): boolean {
  return LIVE_MODES.includes(mode as (typeof LIVE_MODES)[number]);
}
