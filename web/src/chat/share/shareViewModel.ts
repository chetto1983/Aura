// shareViewModel.ts — pure logic extracted from ShareModal.tsx (RESEARCH.md §2's state
// machine, the D-04 expiry math, and the stale-detection affordance), mirroring
// web/src/documents/documentViewModel.ts's split: pure functions, no fetch, no i18n `t()`
// calls, no React. ShareModal.tsx owns rendering + translation; this module owns computation
// only, which keeps both halves independently unit-testable and ShareModal.tsx smaller.

import type { ExpiryOption } from './shareApi';

export type ExpiryChip = '1d' | '7d' | '30d' | 'custom';

/** Mirrors internal/config/config_share.go's defaultShareMaxExpiryDays. A client-side-only UX
 *  guardrail: internal/share/expiry.go's ResolveExpiry CLAMPS an over-cap value rather than
 *  rejecting it, and no endpoint exposes the operator's REAL configured cap to the client, so
 *  a wrong guess here degrades to a slightly early/late aria-invalid hint — never a broken
 *  submit. The server's clamp stays the true authority (the same "cosmetic client hint, real
 *  server gate" posture T-37F-72 already applies to tier gating, applied here to expiry). */
export const DEFAULT_MAX_EXPIRY_DAYS = 90;

const ONE_DAY_MS = 24 * 60 * 60 * 1000;

/** Maps the modal's expiry chip + custom-days input to the wire-level ExpiryOption shareApi
 *  expects: the three presets pass through verbatim; 'custom' becomes the numeric day count
 *  (shareApi.ts's own expiryBody then shapes that into expiry_option:'custom' +
 *  custom_expiry_days on the wire). */
export function expiryOptionForApi(chip: ExpiryChip, customDays: number): ExpiryOption {
  return chip === 'custom' ? customDays : chip;
}

/** True when a custom day count would be REJECTED server-side (<=0,
 *  share.ErrNonPositiveCustomExpiry) or would silently clamp down (> the assumed cap) — either
 *  way, worth flagging before submit. A non-custom chip is never invalid; the three fixed
 *  presets are always in range. */
export function isCustomExpiryInvalid(
  chip: ExpiryChip,
  customDays: number,
  capDays: number = DEFAULT_MAX_EXPIRY_DAYS,
): boolean {
  if (chip !== 'custom') return false;
  return customDays <= 0 || customDays > capDays;
}

/** RESEARCH.md §2's stale-snapshot affordance: "N new messages are not in this link." Both
 *  inputs are optional and forward-compatible (see Conversation.TurnCount and
 *  ShareLink.snapshot_turn_count's own doc comments — internal/agui/share_api.go, plan
 *  37F-10, has not landed yet, so no live response populates either field today). Either
 *  being absent means "no staleness signal available", which this function treats identically
 *  to "not stale" (0) — the same safe degradation the plan's behavior spec describes for "no
 *  newer turns". Never returns a negative count. */
export function computeStaleCount(
  conversation: { readonly TurnCount?: number } | undefined,
  link: { readonly snapshot_turn_count?: number } | undefined,
): number {
  if (conversation?.TurnCount === undefined || link?.snapshot_turn_count === undefined) return 0;
  return Math.max(0, conversation.TurnCount - link.snapshot_turn_count);
}

/** Whole days remaining until `expiresAt`, relative to `now`. Ceils so "23h left" reads as
 *  "1 day" rather than "0 days" (the mockup's "scade tra 7 giorni" is a reassurance, not a
 *  countdown to the second). Returns 0 once expired — the caller renders the expired state
 *  separately via isLinkExpired, never a negative day count. */
export function daysUntil(expiresAt: string, now: Date): number {
  const target = new Date(expiresAt).getTime();
  if (Number.isNaN(target)) return 0;
  return Math.max(0, Math.ceil((target - now.getTime()) / ONE_DAY_MS));
}

/** True once expires_at has passed. Display-only — the store's lazy-resolve predicate (plan
 *  37F-07) is the real security boundary (D-15); this drives the "expired-but-not-swept",
 *  visually-inert row the behavior spec calls for locally, ahead of any sweep. */
export function isLinkExpired(link: { readonly expires_at?: string }, now: Date): boolean {
  if (link.expires_at === undefined) return false;
  const target = new Date(link.expires_at).getTime();
  return !Number.isNaN(target) && target <= now.getTime();
}

/** Absolute same-origin URL for display/copy. ShareLink.url is always a server-shaped
 *  RELATIVE path (`/shared/{id}` or, once, `/s/{token}`) per shareTypes.ts's own contract —
 *  this is presentation-only string concatenation, never a fetch, and constructs nothing the
 *  server didn't already decide (it does not distinguish tier; the path shape already encodes
 *  it). */
export function absoluteShareUrl(relativeUrl: string): string {
  return `${window.location.origin}${relativeUrl}`;
}

/** Locale-formatted absolute date for a title/tooltip attribute — mirrors
 *  web/src/documents/documentViewModel.ts's formatDocumentDate exactly (same
 *  Intl.DateTimeFormat options, same '-' fallback for an empty/invalid value). */
export function formatShareDate(value: string | undefined): string {
  if (value === undefined || value.trim() === '') return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
  }).format(date);
}
