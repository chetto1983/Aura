// Every mode here is a real surface. 'tree' and 'displays' used to sit in this list as
// disabled "coming soon" tabs; a control that has never done anything is not a promise,
// it is clutter that costs a click to discover is dead.
export const MODES = ['chat', 'graph', 'governance', 'documents', 'settings'] as const;

export type SurfaceIntent = (typeof MODES)[number];

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
// the desktop switcher and the mobile nav lists.
export function visibleModes<T extends SurfaceIntent>(modes: readonly T[], isAdmin: boolean): T[] {
  return isAdmin ? [...modes] : modes.filter((mode) => !isAdminMode(mode));
}
