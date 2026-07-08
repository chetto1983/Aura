import { useQuery } from '@tanstack/react-query';
import type { Asset } from '../attachments/types';
import { listThreadAssets } from '../attachments/api';

// useThreadArtifacts (D-10/D-14/WEBART-07): the single derived view behind the
// "Artefatti" panel. It reuses the identity-scoped GET /api/assets?thread_id=
// fetcher (api.ts:47) verbatim as its query fn — no new client, no new source of
// truth — so ownership stays enforced server-side (GetForIdentity → 404 non-owner)
// and a fresh `aura.artifact` frame only has to invalidate ['assets', threadId].
//
// The server returns rows in created_at ASC (the upload fold depends on that order,
// so it MUST NOT be reversed server-side). The panel wants newest-first, agent-only:
// the `select` is where that projection lives — a pure, cache-independent transform
// applied to the shared cache entry, so it never mutates the server's authoritative
// list. It (1) drops user uploads (source_kind !== 'agent'), (2) drops deleted/
// canceled rows, and (3) sorts a COPY newest-first by created_at. `.slice()` guards
// against sorting the frozen cache array in place.

/** The React Query view of a thread's agent-delivered artifacts: agent-only,
 *  deleted/canceled dropped, sorted newest-first over the ASC server list. The
 *  query is disabled until `threadId` is non-empty (no thread → no fetch). */
export function useThreadArtifacts(threadId: string) {
  return useQuery({
    queryKey: ['assets', threadId],
    enabled: threadId.length > 0,
    queryFn: ({ signal }) => listThreadAssets(threadId, signal),
    select: selectAgentArtifacts,
  });
}

/** The pure projection applied to the cached asset list (exported for direct unit
 *  coverage of the filter + newest-first sort without a React render). */
export function selectAgentArtifacts(assets: readonly Asset[]): Asset[] {
  return assets
    .filter((a) => a.source_kind === 'agent' && a.status !== 'deleted' && a.status !== 'canceled')
    .slice()
    .sort((a, b) => (b.created_at ?? '').localeCompare(a.created_at ?? ''));
}
