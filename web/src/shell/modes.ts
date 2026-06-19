export const MODES = ['chat', 'tree', 'graph', 'displays', 'settings'] as const;

export type SurfaceIntent = (typeof MODES)[number];

export const LIVE_MODES = ['chat'] as const satisfies readonly SurfaceIntent[];

export function isLiveSurfaceIntent(mode: SurfaceIntent): boolean {
  return LIVE_MODES.includes(mode as (typeof LIVE_MODES)[number]);
}
