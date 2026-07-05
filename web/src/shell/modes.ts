export const MODES = [
  'chat',
  'tree',
  'graph',
  'governance',
  'documents',
  'displays',
  'settings',
] as const;

export type SurfaceIntent = (typeof MODES)[number];

// 'graph' is live as of Phase 27 — the Frame-06 Graph Explorer workspace (D-11).
// 'governance' is live as of Phase 28 — the read-only MCP/Skills/Scheduler boards (D-01).
// Settings is live as of SETTINGS-02: Postgres-backed runtime/model controls.
// 'tree'/'displays' remain placeholders.
export const LIVE_MODES = [
  'chat',
  'graph',
  'governance',
  'documents',
  'settings',
] as const satisfies readonly SurfaceIntent[];

export function isLiveSurfaceIntent(mode: SurfaceIntent): boolean {
  return LIVE_MODES.includes(mode as (typeof LIVE_MODES)[number]);
}

// ADMIN_MODES are the operator/admin-only surfaces (MUSR-01 / D-03): the model Settings page
// (governance.write-gated writes) and the Governance boards (governance.read/write-gated). A
// non-admin identity holds neither capability, so these are hidden from the nav — a cosmetic
// hide layered over the authoritative server-side RequireCapability gate.
export const ADMIN_MODES = ['governance', 'settings'] as const satisfies readonly SurfaceIntent[];

export function isAdminMode(mode: SurfaceIntent): boolean {
  return ADMIN_MODES.includes(mode as (typeof ADMIN_MODES)[number]);
}

// visibleModes filters the admin-only surfaces out of a mode list for a non-admin identity;
// an admin sees the list unchanged. Generic over the element type so it composes with both
// the full MODES (desktop switcher) and LIVE_MODES (mobile nav) tuples.
export function visibleModes<T extends SurfaceIntent>(modes: readonly T[], isAdmin: boolean): T[] {
  return isAdmin ? [...modes] : modes.filter((mode) => !isAdminMode(mode));
}
