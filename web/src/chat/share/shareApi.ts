// shareApi.ts — the typed client for the share HTTP surface (plan 37F-10, package
// internal/agui). Follows web/src/chat/attachments/api.ts's fetch/credentials/error-shaping
// conventions exactly (Content-Type + Accept headers, credentials: 'same-origin', a thrown
// Error carrying the server's response body on any non-2xx) — no second HTTP idiom is
// invented here.
//
// WRITTEN AGAINST THE LOCKED CONTRACT, NOT A RUNNING SERVER: internal/agui/share_api*.go
// (plan 37F-10, wave 5) has not landed yet — this plan (37F-15) is wave 4. Every route,
// method, and status code below is taken verbatim from
// .planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-10-PLAN.md's locked
// route table. Plan 37F-16 (SharePage.tsx) hit the identical wave-ordering gap and documented
// the same resolution in its own SUMMARY: prove every behavior with a mocked fetch, defer
// live verification to when 37F-10 lands.
//
// TOKEN IS ONE-TIME (D-13): createShare's response carries a public-tier link's plaintext URL
// (`/s/{token}`) EXACTLY ONCE, in ShareLink.url. listShares NEVER returns it again — a public
// row listed later carries `url: undefined`. Render the URL immediately at creation; there is
// nowhere to fetch it back from afterward.
//
// URL SHAPES ARE NOT SYMMETRIC (PRD item 17 / RESEARCH OQ#4 — see shareTypes.ts's own header
// for the full rationale): a public link's `url` is the one-time `/s/{token}` form, present
// ONLY in createShare's response. An internal link's `url` is `/shared/{id}`, carries no
// secret, and is ALWAYS present (including from listShares) — the server computes it; this
// client never constructs a share URL itself for either tier.
//
// WIRE FIELD NAME: the TS parameter is `threadId` (this codebase's frontend convention —
// ArtifactsPanel({threadId}), useThreadArtifacts(threadId)) but the JSON body/query field is
// `conversation_id`, matching internal/share.CreateRequest.ConversationID — share's OWN Go
// domain (unlike internal/assets, which genuinely calls the same concept ThreadID/thread_id
// in ITS OWN package). Two packages, two established names for the same Aura conversation
// entity; this client follows share's, since it wraps share's routes.

import { errorDetail, readJSON } from '../http';
import type { ShareLink } from './shareTypes';

export type Tier = 'internal' | 'public';

/** '1d' | '7d' | '30d' select a fixed preset; a number selects a custom day count (the
 *  server clamps an over-cap value rather than rejecting it — internal/share/expiry.go
 *  ResolveExpiry — but rejects a non-positive one with a 4xx). Omit for the server's 7-day
 *  default (ExpiryDefault). */
export type ExpiryOption = '1d' | '7d' | '30d' | number;

interface CreateShareBody {
  readonly conversation_id: string;
  readonly tier: Tier;
  readonly expiry_option?: '1d' | '7d' | '30d' | 'custom';
  readonly custom_expiry_days?: number;
}

function expiryBody(
  expiryOption: ExpiryOption | undefined,
): Pick<CreateShareBody, 'expiry_option' | 'custom_expiry_days'> {
  if (expiryOption === undefined) return {};
  if (typeof expiryOption === 'number') {
    return { expiry_option: 'custom', custom_expiry_days: expiryOption };
  }
  return { expiry_option: expiryOption };
}

/** POST /api/shares — mints a share for threadId. The public tier's response carries the
 *  one-time plaintext URL; the internal tier's URL is the server-derived /shared/{id} form. */
export async function createShare(
  threadId: string,
  tier: Tier,
  expiryOption?: ExpiryOption,
  signal?: AbortSignal,
): Promise<ShareLink> {
  const body: CreateShareBody = {
    conversation_id: threadId,
    tier,
    ...expiryBody(expiryOption),
  };
  const init: RequestInit = {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  };
  if (signal !== undefined) init.signal = signal;
  const res = await fetch('/api/shares', init);
  return readJSON<ShareLink>(res);
}

/** GET /api/shares(?conversation_id=) — the owner's links, optionally scoped to one thread. */
export async function listShares(
  threadId?: string,
  signal?: AbortSignal,
): Promise<readonly ShareLink[]> {
  const qs =
    threadId !== undefined && threadId.length > 0
      ? `?conversation_id=${encodeURIComponent(threadId)}`
      : '';
  const init: RequestInit = {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  };
  if (signal !== undefined) init.signal = signal;
  const res = await fetch(`/api/shares${qs}`, init);
  const value = await readJSON<unknown>(res);
  return Array.isArray(value) ? (value as ShareLink[]) : [];
}

/** PATCH /api/shares/{id}/snapshot — re-snapshots, keeping the SAME token/URL (D-06). */
export async function updateShareSnapshot(id: string, signal?: AbortSignal): Promise<ShareLink> {
  const init: RequestInit = {
    method: 'PATCH',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  };
  if (signal !== undefined) init.signal = signal;
  const res = await fetch(`/api/shares/${encodeURIComponent(id)}/snapshot`, init);
  return readJSON<ShareLink>(res);
}

/** DELETE /api/shares/{id} — revokes; resolves void on any 2xx. */
export async function revokeShare(id: string, signal?: AbortSignal): Promise<void> {
  const init: RequestInit = {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  };
  if (signal !== undefined) init.signal = signal;
  const res = await fetch(`/api/shares/${encodeURIComponent(id)}`, init);
  if (!res.ok) throw new Error(await errorDetail(res));
}
