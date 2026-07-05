// adminApi.ts is the MUSR-01 (D-03/D-26/D-28) admin/user-distinction data layer over the
// Phase-36 backend routes (internal/agui/audit_api.go). All calls are credentials:
// 'same-origin' via the shared getJSON/postJSON helpers — the SPA is served by the very
// binary exposing the routes, behind the RequireAuth whole-origin gate; the four /api/admin/*
// routes are additionally RequireCapability(governance.write)-gated server-side, so a
// non-admin fetch fails closed regardless of what the SPA renders.

import { getJSON, postJSON } from '../api/json';

/** The capability that marks an "admin" identity (D-01/D-03). */
export const GOVERNANCE_WRITE = 'governance.write';

/** The system-managed wildcard grant — the seeded `local` operator holds it (migration 0004). */
export const WILDCARD = '*';

/** GET /api/me — the caller's own principal + capability set (self-scoped). */
export interface MeResponse {
  readonly identity_id: string;
  readonly capabilities: readonly string[];
}

/** One row of GET /api/admin/identities — an identity + its current grants. */
export interface AdminIdentity {
  readonly id: string;
  readonly name: string;
  readonly kind: string;
  readonly capabilities: readonly string[];
}

/** One normalized event of GET /api/admin/audit — the D-28 per-user activity feed. */
export interface AuditEvent {
  readonly source: string;
  readonly action: string;
  readonly target: string;
  readonly detail?: string;
  readonly created_at: string;
}

export interface AuditPage {
  readonly events: readonly AuditEvent[];
  readonly limit: number;
  readonly offset: number;
}

export interface CapabilityMutationResult {
  readonly identity_id: string;
  readonly capabilities: readonly string[];
}

/**
 * hasCapability mirrors the backend HasCapability semantics: the '*' wildcard grants
 * everything, otherwise an exact match. The SPA uses it to gate admin surfaces — a cosmetic
 * hide layered over the authoritative server-side RequireCapability gate.
 */
export function hasCapability(capabilities: readonly string[], capability: string): boolean {
  return capabilities.includes(WILDCARD) || capabilities.includes(capability);
}

export function fetchMe(): Promise<MeResponse> {
  return getJSON<MeResponse>('/api/me');
}

export async function fetchAdminIdentities(): Promise<readonly AdminIdentity[]> {
  const res = await getJSON<{ identities?: readonly AdminIdentity[] }>('/api/admin/identities');
  return res.identities ?? [];
}

export function grantCapability(
  identityId: string,
  capability: string,
): Promise<CapabilityMutationResult> {
  return postJSON<CapabilityMutationResult>(
    `/api/admin/identities/${encodeURIComponent(identityId)}/capabilities`,
    { capability },
  );
}

export async function revokeCapability(
  identityId: string,
  capability: string,
): Promise<CapabilityMutationResult> {
  const res = await fetch(
    `/api/admin/identities/${encodeURIComponent(identityId)}/capabilities/${encodeURIComponent(capability)}`,
    { method: 'DELETE', headers: { Accept: 'application/json' }, credentials: 'same-origin' },
  );
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  return (await res.json()) as CapabilityMutationResult;
}

export function fetchAudit(identityId: string, limit: number, offset: number): Promise<AuditPage> {
  const query = new URLSearchParams({
    identity: identityId,
    limit: String(limit),
    offset: String(offset),
  });
  return getJSON<AuditPage>(`/api/admin/audit?${query.toString()}`);
}
