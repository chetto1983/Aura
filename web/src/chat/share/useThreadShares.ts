import { useQuery } from '@tanstack/react-query';
import { listShares } from './shareApi';
import type { ShareLink } from './shareTypes';

// useThreadShares (D-05, the "Condiviso" section 37B deferred to this phase) mirrors
// useThreadArtifacts.ts's shape exactly: a useQuery keyed ['shares', threadId], disabled for an
// empty threadId, and a PURE exported `selectActiveShares` projection. The select is exported
// for the same reason useThreadArtifacts.ts documents for its own selectAgentArtifacts: it lets
// the filter+sort be unit-tested without a React render.
//
// The projection drops revoked links only — it does NOT drop expired ones. An
// expired-but-not-yet-reclaimed link must still render, as expired and visually inert: expiry
// is a display concern the row reads from `expires_at` at render time, never a filter applied
// here and never a background-job timestamp.

/** The React Query view of a thread's share links: revoked links dropped, newest-first. The
 *  query is disabled until `threadId` is non-empty (no thread -> no fetch). */
export function useThreadShares(threadId: string) {
  return useQuery({
    queryKey: ['shares', threadId],
    enabled: threadId.length > 0,
    queryFn: ({ signal }) => listShares(threadId, signal),
    select: selectActiveShares,
  });
}

/** The pure projection applied to the cached share list (exported for direct unit coverage of
 *  the filter + newest-first sort without a React render). Drops revoked links; keeps expired
 *  ones so the caller can render them inert rather than silently hiding them. */
export function selectActiveShares(links: readonly ShareLink[]): ShareLink[] {
  return links
    .filter((l) => l.revoked_at === undefined)
    .slice()
    .sort((a, b) => b.created_at.localeCompare(a.created_at));
}
