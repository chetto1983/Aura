import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { APPROVAL_GRANTS_KEY } from './useApprovalGrants';

// useApprovalPolicies is the data layer over the per-identity ask/deny tool policies
// (/api/approvals/policies, prd.md §5, 2026-10-09). It mirrors useApprovalGrants.ts: React
// Query, credentials: 'same-origin', retry: false so a failure surfaces.
//
//   GET  /api/approvals/policies        → the authenticated principal's policies
//   PUT  /api/approvals/policies        → set ask or deny on one subject
//   POST /api/approvals/policies/clear  → drop one, by its two coordinates
//
// Setting ask revokes a standing "always" grant on the same subject server-side, so a PUT
// also invalidates the grants list.

export type ToolPolicy = 'ask' | 'deny';

export interface ApprovalPolicy {
  readonly tool: string;
  readonly action: string;
  readonly subject: string;
  readonly policy: ToolPolicy;
  readonly set_at: string;
  readonly set_by?: string;
}

export const APPROVAL_POLICIES_KEY = 'approval-policies';

const POLICIES_PATH = '/api/approvals/policies';

async function send(method: string, path: string, body?: unknown): Promise<unknown> {
  const init: RequestInit =
    body === undefined
      ? { method, headers: { Accept: 'application/json' }, credentials: 'same-origin' }
      : {
          method,
          headers: { 'Content-Type': 'application/json' },
          credentials: 'same-origin',
          body: JSON.stringify(body),
        };
  const res = await fetch(path, init);
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  return res.json();
}

async function fetchApprovalPolicies(): Promise<ApprovalPolicy[]> {
  const body = await send('GET', POLICIES_PATH);
  return Array.isArray(body) ? (body as ApprovalPolicy[]) : [];
}

export interface SetPolicyVars {
  readonly tool: string;
  readonly action: string;
  readonly policy: ToolPolicy;
}

export interface ClearPolicyVars {
  readonly tool: string;
  readonly action: string;
}

/** GET /api/approvals/policies — the policies of the authenticated principal. */
export function useApprovalPolicies() {
  return useQuery({
    queryKey: [APPROVAL_POLICIES_KEY],
    queryFn: fetchApprovalPolicies,
    retry: false,
  });
}

/** PUT /api/approvals/policies — set ask or deny, then refresh policies and grants. */
export function useSetApprovalPolicy() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: SetPolicyVars) => send('PUT', POLICIES_PATH, vars),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [APPROVAL_POLICIES_KEY] });
      void queryClient.invalidateQueries({ queryKey: [APPROVAL_GRANTS_KEY] });
    },
  });
}

/** POST /api/approvals/policies/clear — drop one policy and refresh the list. */
export function useClearApprovalPolicy() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: ClearPolicyVars) => send('POST', `${POLICIES_PATH}/clear`, vars),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [APPROVAL_POLICIES_KEY] });
    },
  });
}
