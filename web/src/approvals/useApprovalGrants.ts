import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

// useApprovalGrants is the data layer over the standing "always approve" grants
// (/api/approvals/grants, PRD amendment #127). It mirrors useApprovals.ts: React Query,
// always credentials: 'same-origin', retry: false so a failure surfaces instead of
// silently spinning.
//
//   GET  /api/approvals/grants         → the authenticated principal's standing grants
//   POST /api/approvals/grants/revoke  → drop one, by its two coordinates
//
// It does NOT poll. A grant changes only when the operator approves one or revokes one,
// both of which invalidate this query directly — unlike the pending queue, where a
// background run can create work nobody clicked for.

/**
 * GET /api/approvals/grants row. `subject` is rendered server-side by the same function
 * that named the approval option, so what the operator revokes reads exactly like what
 * they approved; tool/action are its two coordinates and are what revoke sends back.
 */
export interface ApprovalGrant {
  readonly tool: string;
  readonly action: string;
  readonly subject: string;
  readonly granted_at: string;
  readonly granted_by?: string;
}

export const APPROVAL_GRANTS_KEY = 'approval-grants';

async function fetchApprovalGrants(): Promise<ApprovalGrant[]> {
  const res = await fetch('/api/approvals/grants', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  const body: unknown = await res.json();
  return Array.isArray(body) ? (body as ApprovalGrant[]) : [];
}

export interface RevokeGrantVars {
  readonly tool: string;
  readonly action: string;
}

/** The server reports whether a row was actually removed, so the UI never claims a revoke
 *  that hit nothing (a stale list, or a grant revoked in another tab). */
export interface RevokeResult {
  readonly revoked: boolean;
}

async function postRevokeGrant(vars: RevokeGrantVars): Promise<RevokeResult> {
  const res = await fetch('/api/approvals/grants/revoke', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ tool: vars.tool, action: vars.action }),
  });
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  const parsed: unknown = await res.json();
  return parsed as RevokeResult;
}

/** GET /api/approvals/grants — the standing grants of the authenticated principal. */
export function useApprovalGrants() {
  return useQuery({
    queryKey: [APPROVAL_GRANTS_KEY],
    queryFn: fetchApprovalGrants,
    retry: false,
  });
}

/** POST /api/approvals/grants/revoke — drop one standing grant and refresh the list. */
export function useRevokeApprovalGrant() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: postRevokeGrant,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [APPROVAL_GRANTS_KEY] });
    },
  });
}
