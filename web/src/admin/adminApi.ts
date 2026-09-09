// adminApi.ts is the MUSR-01 (D-03/D-26/D-28) admin/user-distinction data layer over the
// Phase-36 backend routes (internal/agui/audit_api.go) plus Phase 2's identity-removal and
// per-identity credit routes (internal/agui/deprovision_route.go, internal/agui/credit_api.go).
// All calls are credentials: 'same-origin' via the shared getJSON/postJSON helpers — the SPA is
// served by the very binary exposing the routes, behind the RequireAuth whole-origin gate.
//
// Gate asymmetry (D-01/D-02/RBAC-06, Phase 2): the four original /api/admin/* routes are still
// RequireCapability(governance.write)-gated server-side, but every identity now holds
// governance.write — that gate no longer distinguishes an admin from a member on THOSE routes.
// The routes that still do are the two this file adds: DELETE /api/admin/identities/{id}
// (RequireCapability(identity.delete)) and the identity-creation leg of onboarding
// (RequireCapability(identity.create)). A non-admin fetch to either fails closed server-side
// regardless of what the SPA renders; the SPA's own isAdmin hide (useAdmin.ts) is cosmetic.

import { getJSON, httpErrorFrom, postJSON } from '../api/json';

/** The two capabilities identity.Administrative() grants at bootstrap and nowhere else
 * (D-02/RBAC-06) — mint/remove an identity, set its credit. These are what useAdmin.ts's
 * isAdmin now derives from. */
export const IDENTITY_CREATE = 'identity.create';
export const IDENTITY_DELETE = 'identity.delete';

/**
 * GET /api/me — the caller's own principal + capability set (self-scoped).
 * context_window is the active model's total context window (tokens, llm.Config.
 * ContextWindow) the footer gauge reads instead of hardcoding the DeepSeek-V4 1M default
 * (footerMetrics.DEFAULT_CONTEXT_WINDOW); 0/absent means the daemon hasn't wired it.
 */
export interface MeResponse {
  readonly identity_id: string;
  /** The identity's own name (an email for a human identity) — the same value the roster
   * renders. CRED-05's turn refusal names the identity it refused and has no other source:
   * authentication is external, so the SPA never sees the login email. Empty when the server
   * could not resolve it; a caller must not interpolate a blank into copy. */
  readonly name?: string;
  readonly capabilities: readonly string[];
  readonly context_window?: number;
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
  /** Tool-leg pairing key (tool_call_id); absent on mcp/skill/share legs. */
  readonly correlation?: string;
  /** Tool-leg persisted duration (end rows); absent/0 elsewhere. */
  readonly duration_ms?: number;
  readonly created_at: string;
}

export interface AuditPage {
  readonly events: readonly AuditEvent[];
  readonly limit: number;
  readonly offset: number;
}

/**
 * hasCapability is an exact-match membership check — no wildcard branch. The backend stopped
 * expanding the '*' wildcard in plan 02-01 (D-01); a client that still expanded it here would
 * grant itself capabilities the server refuses, showing an action the server then rejects,
 * which is worse than not showing it at all. The SPA uses this to gate admin surfaces — a
 * cosmetic hide layered over the authoritative server-side RequireCapability gate.
 */
export function hasCapability(capabilities: readonly string[], capability: string): boolean {
  return capabilities.includes(capability);
}

export function fetchMe(): Promise<MeResponse> {
  return getJSON<MeResponse>('/api/me');
}

export async function fetchAdminIdentities(): Promise<readonly AdminIdentity[]> {
  const res = await getJSON<{ identities?: readonly AdminIdentity[] }>('/api/admin/identities');
  return res.identities ?? [];
}

export function fetchAudit(identityId: string, limit: number, offset: number): Promise<AuditPage> {
  const query = new URLSearchParams({
    identity: identityId,
    limit: String(limit),
    offset: String(offset),
  });
  return getJSON<AuditPage>(`/api/admin/audit?${query.toString()}`);
}

/**
 * GET /api/admin/identities/{id}/credit response (CRED-03/CRED-06/CRED-09,
 * internal/agui/credit_api.go). `cap` arrives as a plain JSON number (the server's USDCap
 * marshals a fixed two-decimal token, but JSON has no fixed-decimal number type, so the wire
 * value loses its trailing zero the moment a JS number parses it — callers must re-apply
 * .toFixed(2) themselves). `spend`/`remaining` are FULL ledger precision, deliberately not
 * rounded to cents (migration 0124, aura.cache_metrics.cost_usd numeric(24,12)) — a caller
 * that rounds a sub-cent spend to "0.00" reintroduces the exact defect that migration fixed.
 * A local, non-billing backend returns the `exempt` shape instead, with none of the other
 * fields (D-13) — this is CRED-09's exemption, never a fabricated zero cap.
 */
export interface CreditExempt {
  readonly identity_id: string;
  readonly exempt: true;
}

export interface CreditRecord {
  readonly identity_id: string;
  readonly exempt: false;
  readonly cap: number;
  readonly reset_interval: string;
  readonly spend: number;
  readonly remaining: number;
  readonly percent_used: number;
}

export type CreditResponse = CreditExempt | CreditRecord;

/** POST /api/admin/identities/{id}/credit body — either field may be omitted to leave it
 * unchanged (server-side nil-means-unchanged convention, credit_api.go's creditSetRequest). */
export interface CreditSetPatch {
  readonly cap?: string;
  readonly reset_interval?: string;
}

/** POST .../credit response — the APPLIED cap (server-rounded HALF-UP to cents), never the
 * raw typed value. A caller that displays the request body instead of this response would
 * show 5.126 when the provider and the store both hold 5.13. */
export interface CreditSetResult {
  readonly identity_id: string;
  readonly cap: number;
  readonly reset_interval: string;
  readonly store_applied: boolean;
  readonly provider_applied: boolean;
}

export function fetchIdentityCredit(identityId: string): Promise<CreditResponse> {
  return getJSON<CreditResponse>(`/api/admin/identities/${encodeURIComponent(identityId)}/credit`);
}

export function setIdentityCredit(
  identityId: string,
  patch: CreditSetPatch,
): Promise<CreditSetResult> {
  return postJSON<CreditSetResult>(
    `/api/admin/identities/${encodeURIComponent(identityId)}/credit`,
    patch,
  );
}

/** DELETE /api/admin/identities/{id} response (RBAC-05, internal/agui/deprovision_route.go).
 * The teardown runs synchronously — a 200 means every plane converged (deactivate + full
 * purge); there is no accepted-with-status shape to poll. */
export interface RemoveIdentityResult {
  readonly identity_id: string;
  readonly status: string;
}

/**
 * DELETE /api/admin/identities/{id} — removes an identity across every plane (RBAC-05).
 * Uses httpErrorFrom (not a bare Error) so a 403 (identity.ErrLastAdministrator, "the last
 * administrative identity cannot remove or deactivate itself") surfaces its own server-given
 * reason rather than a bare "HTTP 403" — the roster disables this control unconditionally for
 * the admin's own row (D-03), so this path is a defensive backstop, not the normal case.
 */
export async function removeIdentity(identityId: string): Promise<RemoveIdentityResult> {
  const res = await fetch(`/api/admin/identities/${encodeURIComponent(identityId)}`, {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  if (!res.ok) {
    throw await httpErrorFrom(res);
  }
  return (await res.json()) as RemoveIdentityResult;
}
