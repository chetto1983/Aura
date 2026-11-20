// Every mode here is a real surface. 'tree' and 'displays' used to sit in this list as
// disabled "coming soon" tabs; a control that has never done anything is not a promise,
// it is clutter that costs a click to discover is dead.
export const MODES = [
  'chat',
  'board',
  'studio',
  'graph',
  'governance',
  'documents',
  'settings',
] as const;

export type SurfaceIntent = (typeof MODES)[number];

// What each surface says while its lazy chunk loads.
export const SURFACE_LOADING = {
  chat: 'chat.loading',
  board: 'board.loading',
  studio: 'studio.loading',
  graph: 'graph.loading',
  governance: 'governance.loading',
  documents: 'files.loading',
  settings: 'settings.loading',
} as const satisfies Record<SurfaceIntent, string>;

// ADMIN_MODES are the administrator-only surfaces (MUSR-01 / D-03), hidden from the nav of an
// identity without identity.create — a cosmetic hide layered over the authoritative server-side
// RequireCapability gate. Settings is not one since 2026-11-20 (spec A1): every identity owns its
// profile and its mail accounts, and the section rail drops the admin panes for a member.
export const ADMIN_MODES = ['governance'] as const satisfies readonly SurfaceIntent[];

export function isAdminMode(mode: SurfaceIntent): boolean {
  return ADMIN_MODES.includes(mode as (typeof ADMIN_MODES)[number]);
}

// visibleModes filters the admin-only surfaces out of a mode list for a non-admin identity;
// an admin sees the list unchanged. Generic over the element type so it composes with both
// the desktop switcher and the mobile nav lists.
export function visibleModes<T extends SurfaceIntent>(modes: readonly T[], isAdmin: boolean): T[] {
  return isAdmin ? [...modes] : modes.filter((mode) => !isAdminMode(mode));
}
