// useAdmin.ts is the React Query data layer for the MUSR-01 admin surfaces plus Phase 2's
// identity-removal and per-identity credit surfaces. It mirrors useConversations.ts:
// useQuery/useMutation over the thin adminApi REST adapter, retry:false so a 403/failure
// surfaces immediately (a non-admin's /api/admin/* fetch is denied server-side), and
// mutations invalidate the roster (and, for credit, the identity's own credit read) so a
// change reflects at once.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  IDENTITY_CREATE,
  fetchAdminIdentities,
  fetchAudit,
  fetchIdentityCredit,
  fetchMe,
  hasCapability,
  removeIdentity,
  setIdentityCredit,
  type AdminIdentity,
  type CreditSetPatch,
} from './adminApi';

const IDENTITIES_KEY = ['admin', 'identities'] as const;

/**
 * useCapabilities reads the caller's own capabilities (GET /api/me) and derives isAdmin from
 * identity.create (D-02/RBAC-06's administrative pair — identity.create and identity.delete
 * are always granted together at bootstrap, so checking either one is equivalent). Every
 * identity holds governance.write as of D-01, so that capability no longer distinguishes an
 * admin from a member — deriving isAdmin from it would show every user the admin surface the
 * moment it lands (the regression this derivation exists to avoid). Fails CLOSED: while
 * loading or on error, isAdmin is false, so admin surfaces never flash to a user whose grants
 * are unconfirmed.
 */
export function useCapabilities() {
  const query = useQuery({
    queryKey: ['me'],
    queryFn: fetchMe,
    retry: false,
    staleTime: 60_000,
  });
  const capabilities = query.data?.capabilities ?? [];
  const rawWindow = query.data?.context_window;
  return {
    capabilities,
    identityId: query.data?.identity_id ?? '',
    identityName: query.data?.name ?? '',
    isAdmin: hasCapability(capabilities, IDENTITY_CREATE),
    isLoading: query.isLoading,
    isError: query.isError,
    // contextWindow is undefined while loading/erroring/unwired (context_window <= 0) so
    // RuntimeFooter's own DEFAULT_CONTEXT_WINDOW fallback applies instead of a false 0-token gauge.
    contextWindow:
      rawWindow !== undefined && Number.isFinite(rawWindow) && rawWindow > 0
        ? rawWindow
        : undefined,
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

/** useAudit reads the per-user activity feed for the selected identity (D-28). */
export function useAudit(identityId: string, limit: number, offset: number) {
  return useQuery({
    queryKey: ['admin', 'audit', identityId, limit, offset],
    queryFn: () => fetchAudit(identityId, limit, offset),
    enabled: identityId !== '',
    retry: false,
  });
}

const creditKey = (identityId: string) => ['admin', 'credit', identityId] as const;

/** useIdentityCredit reads one identity's cap/reset/remaining/spend (CRED-03/CRED-06), or the
 * CRED-09 exemption shape on a non-billing backend. Disabled until an identity id is known. */
export function useIdentityCredit(identityId: string) {
  return useQuery({
    queryKey: creditKey(identityId),
    queryFn: () => fetchIdentityCredit(identityId),
    enabled: identityId !== '',
    retry: false,
  });
}

/** useSetIdentityCredit saves a cap/reset-interval change and refreshes both this identity's
 * credit read and the roster (a cap change can affect what the roster's own summary shows). */
export function useSetIdentityCredit() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: { identityId: string; patch: CreditSetPatch }) =>
      setIdentityCredit(vars.identityId, vars.patch),
    onSuccess: (_result, vars) => {
      void client.invalidateQueries({ queryKey: creditKey(vars.identityId) });
      void client.invalidateQueries({ queryKey: IDENTITIES_KEY });
    },
  });
}

/** useRemoveIdentity runs the RBAC-05 removal saga and refreshes the roster on success. */
export function useRemoveIdentity() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (identityId: string) => removeIdentity(identityId),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: IDENTITIES_KEY });
    },
  });
}
