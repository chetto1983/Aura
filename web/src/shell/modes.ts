export const MODES = ['chat', 'tree', 'graph', 'displays', 'settings'] as const;

export type SurfaceIntent = (typeof MODES)[number];

// 'graph' is live as of Phase 27 — the Frame-06 Graph Explorer workspace (D-11). It is the
// second reachable surface beside chat; 'tree'/'displays'/'settings' remain placeholders.
export const LIVE_MODES = ['chat', 'graph'] as const satisfies readonly SurfaceIntent[];

export function isLiveSurfaceIntent(mode: SurfaceIntent): boolean {
  return LIVE_MODES.includes(mode as (typeof LIVE_MODES)[number]);
}
