import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

// useApprovals is the APRV-01/02/03 data layer over the plan-25-02 thin REST
// adapter (/api/approvals). It mirrors useRuntimeHealth.ts / useConversations.ts:
// React Query useQuery/useMutation, ALWAYS credentials: 'same-origin' (the SPA is
// served by the very binary exposing the routes, behind the Phase-24 RequireAuth
// whole-origin gate), retry: false so a transient failure surfaces immediately.
//
//   GET  /api/approvals                  → the cross-thread pending queue (badge + list)
//   POST /api/approvals/{token}/resolve  → accept|decline|cancel resume bridge
//
// The list poll (refetchInterval, mirrored from useRuntimeHealth.ts:61) drives the
// header badge count even when the operator is not viewing the pending thread — an
// ask_user can fire in a background/scheduled run (D-04). The resolve mutation is
// capability-gated server-side (plan 25-02): a 403 surfaces as isError, never a
// silent no-op.

/**
 * GET /api/approvals row — the agui approvalItem projection (plan 25-02
 * approvals_api.go). The question is server-sanitized (SanitizeString, V7). A
 * background/scheduled interrupt carries the owning conversation id so the list can
 * jump straight to that thread's inline card.
 */
export interface Approval {
  readonly token: string;
  readonly conversation_id: string;
  readonly kind: string;
  /** Server-sanitized ask_user question — rendered VERBATIM, no client rewrite. */
  readonly question: string;
  /** Raw JSON option set (string[] | null) the pause offered; absent for free-form. */
  readonly options?: unknown;
  readonly priority: number;
  /** Proxied-from-child (flat swarm-worker) id; "" / absent for a direct call. */
  readonly source?: string;
  /** D-06: a terminal (expired / auto-resolved) marker so the row never vanishes. */
  readonly terminal?: boolean;
}

/** The three resolve verbs (D-05) — 1:1 with askuser.Action* (store.go:35-37). */
export type ResolveAction = 'accept' | 'decline' | 'cancel';

export interface ResolveVars {
  readonly token: string;
  readonly action: ResolveAction;
  /**
   * The operator's answer text — sent ONLY for accept. For decline/cancel the
   * server injects its own content (declinedContent / auto-resolve marker), so a
   * caller MUST NOT pass operator text on a decline (deny != accept, T-25-17).
   */
  readonly content?: string;
}

export const APPROVALS_KEY = 'approvals';
export const APPROVALS_REFETCH_INTERVAL_MS = 5000;

async function fetchApprovals(): Promise<Approval[]> {
  const res = await fetch('/api/approvals', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  // The endpoint returns a JSON array; coerce defensively so a non-array body
  // (e.g. an unexpected envelope) can never crash a downstream .filter/.length.
  const body: unknown = await res.json();
  return Array.isArray(body) ? (body as Approval[]) : [];
}

// The resolve POST returns 204 No Content; a non-2xx (incl. the 403 capability
// gate and the 404 already-resolved) is a thrown error the mutation surfaces.
async function postResolve(vars: ResolveVars): Promise<void> {
  // accept carries the operator content; decline/cancel send NO content — the
  // server owns the declined/auto-resolve text (T-25-17 footgun guard at the wire).
  const body =
    vars.action === 'accept' ? { action: vars.action, content: vars.content ?? '' } : { action: vars.action };
  const res = await fetch(`/api/approvals/${encodeURIComponent(vars.token)}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
}

/**
 * GET /api/approvals — the polled cross-thread pending queue driving the header
 * badge count + the lightweight list (D-04). The poll keeps the count live for a
 * background/scheduled interrupt the operator is not viewing.
 */
export function useApprovals() {
  return useQuery({
    queryKey: [APPROVALS_KEY],
    queryFn: fetchApprovals,
    refetchInterval: APPROVALS_REFETCH_INTERVAL_MS,
    retry: false,
  });
}

/**
 * POST /api/approvals/{token}/resolve — the three-verb resume bridge (D-05). On
 * success it invalidates the approvals query so the badge/list drops the resolved
 * row immediately (and the inline card transitions to its terminal chip).
 */
export function useResolveApproval() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: postResolve,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [APPROVALS_KEY] });
    },
  });
}
