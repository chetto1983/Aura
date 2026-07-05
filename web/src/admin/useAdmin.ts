// useAdmin.ts is the React Query data layer for the MUSR-01 admin surfaces. It mirrors
// useConversations.ts: useQuery/useMutation over the thin adminApi REST adapter, retry:false
// so a 403/failure surfaces immediately (a non-admin's /api/admin/* fetch is denied
// server-side), and mutations invalidate the roster so a grant/revoke reflects at once.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  GOVERNANCE_WRITE,
  fetchAdminIdentities,
  fetchAudit,
  fetchMe,
  grantCapability,
  hasCapability,
  revokeCapability,
  type AdminIdentity,
} from './adminApi';

const IDENTITIES_KEY = ['admin', 'identities'] as const;

/**
 * useCapabilities reads the caller's own capabilities (GET /api/me) and derives isAdmin
 * (holds governance.write or the '*' wildcard). It fails CLOSED: while loading or on error,
 * isAdmin is false, so admin surfaces never flash to a user whose grants are unconfirmed.
 */
export function useCapabilities() {
  const query = useQuery({
    queryKey: ['me'],
    queryFn: fetchMe,
    retry: false,
    staleTime: 60_000,
  });
  const capabilities = query.data?.capabilities ?? [];
  return {
    capabilities,
    identityId: query.data?.identity_id ?? '',
    isAdmin: hasCapability(capabilities, GOVERNANCE_WRITE),
    isLoading: query.isLoading,
    isError: query.isError,
  };
}

/** useAdminIdentities reads the identity roster + each identity's grants (admin only). */
export function useAdminIdentities() {
  return useQuery<readonly AdminIdentity[]>({
    queryKey: IDENTITIES_KEY,
    queryFn: fetchAdminIdentities,
    retry: false,
  });
}

/** useGrantCapability grants a capability and refreshes the roster on success (D-26). */
export function useGrantCapability() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: { identityId: string; capability: string }) =>
      grantCapability(vars.identityId, vars.capability),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: IDENTITIES_KEY });
    },
  });
}

/** useRevokeCapability revokes a capability and refreshes the roster on success (D-26). */
export function useRevokeCapability() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: { identityId: string; capability: string }) =>
      revokeCapability(vars.identityId, vars.capability),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: IDENTITIES_KEY });
    },
  });
}

/** useAudit reads the per-user activity feed for the selected identity (D-28). */
export function useAudit(identityId: string, limit: number, offset: number) {
  return useQuery({
    queryKey: ['admin', 'audit', identityId, limit, offset],
    queryFn: () => fetchAudit(identityId, limit, offset),
    enabled: identityId !== '',
    retry: false,
  });
}
