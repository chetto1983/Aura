// shareApi.ts — the typed client for the share HTTP surface (plan 37F-10, package
// internal/agui). Follows web/src/chat/attachments/api.ts's fetch/credentials/error-shaping
// conventions exactly (Content-Type + Accept headers, credentials: 'same-origin', a thrown
// Error carrying the server's response body on any non-2xx) — no second HTTP idiom is
// invented here.
//
// RED STUB (TDD task 1, plan 37F-15): signatures only, every body throws. Implemented in the
// GREEN commit that follows.

import type { ShareLink } from './shareTypes';

export type Tier = 'internal' | 'public';

export type ExpiryOption = '1d' | '7d' | '30d' | number;

export function createShare(
  _threadId: string,
  _tier: Tier,
  _expiryOption?: ExpiryOption,
  _signal?: AbortSignal,
): Promise<ShareLink> {
  throw new Error('not implemented');
}

export function listShares(_threadId?: string, _signal?: AbortSignal): Promise<readonly ShareLink[]> {
  throw new Error('not implemented');
}

export function updateShareSnapshot(_id: string, _signal?: AbortSignal): Promise<ShareLink> {
  throw new Error('not implemented');
}

export function revokeShare(_id: string, _signal?: AbortSignal): Promise<void> {
  throw new Error('not implemented');
}
