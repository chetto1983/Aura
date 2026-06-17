export const MODES = ['chat', 'tree', 'graph', 'displays', 'settings'] as const;

export type SurfaceIntent = (typeof MODES)[number];
