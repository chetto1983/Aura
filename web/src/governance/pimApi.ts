// pimApi.ts is the cockpit "Connect" data layer for calendar/PIM account-linking (connect_pim_api.go).
// Split out of governanceApi.ts (which crossed the 600-LOC cap) as its own cohesive concern. The
// /api/connect/pim/* routes proxy the aura-pim-mcp sidecar's token-gated /admin REST API behind the
// SAME governance.write gate, injecting the admin Bearer token server-side (it never crosses the wire).
// It reuses governanceApi's getJSON/postJSON/deleteJSON (same-origin, Accept: application/json, a
// non-200 — incl. 401/503 sidecar-unconfigured — THROWS `Error("HTTP <n>")`) so an offline/error state
// surfaces visibly in the connect section. The operator enters their OWN provider credentials in the
// wizard form (Google/Microsoft OAuth client, IMAP creds, an ICS URL, …) — nothing is read from env.
// The wizard supports every provider the sidecar exposes, with two connect flows: Google = web-redirect
// (pimGoogleStart), Microsoft/Outlook = device-code (pimDeviceStart + pimAuthStatus poll).

import { getJSON, isTrue } from '../api/json';
import { deleteJSON, postJSON, stringValue } from './governanceApi';

export const GOV_PIM_ACCOUNTS_PATH = '/api/connect/pim/accounts';

/** The provider ids the sidecar's AccountValidation.KnownProviders accepts. Sent verbatim as the
 * account `provider` field. (`outlook.com` carries a dot, so its i18n label key is decoupled to
 * `outlookCom` in pimProviders.ts to avoid the i18next key separator.) */
export type PimProviderId = 'google' | 'microsoft365' | 'outlook.com' | 'imap' | 'ics' | 'json';

/** One calendar/PIM account row (GET /api/connect/pim/accounts). The sidecar NEVER echoes stored
 * secrets — only this redacted projection reaches the wire. */
export interface PimAccount {
  readonly id: string;
  readonly displayName: string;
  readonly provider: string;
  readonly enabled: boolean;
}

/** GET /api/connect/pim/accounts response. */
export interface PimAccountList {
  readonly accounts: readonly PimAccount[];
}

/** GET …/google/start response — the consent URL the operator opens + the exact redirect URI they
 * must register in their Google Cloud OAuth client. */
export interface PimGoogleStart {
  readonly authUrl: string;
  readonly redirectUri: string;
}

/** POST …/{id}/auth/start response — the Microsoft/Outlook device-code grant. The operator opens
 * verificationUrl in a new tab and enters userCode. */
export interface PimDeviceStart {
  readonly userCode: string;
  readonly verificationUrl: string;
  readonly message: string;
  readonly expiresIn: number;
}

/** GET …/{id}/auth/status response — the live device-code flow state the wizard polls
 * (pending | awaiting_user | completed | failed | cancelled | not_found). */
export interface PimAuthStatus {
  readonly status: string;
  readonly message: string;
  readonly userCode?: string | undefined;
  readonly verificationUrl?: string | undefined;
}

/** POST /api/connect/pim/accounts request body. provider is one of PimProviderId; providerConfig
 * carries the exact lowercase keys the sidecar provider service reads (clientId/clientSecret,
 * tenantId, icsUrl, imapHost/smtpHost/username/password, source/filePath/oneDrivePath, …).
 * domains/priority are optional account-routing hints (omitted when unset). */
export interface PimCreateAccountRequest {
  readonly id: string;
  readonly displayName: string;
  readonly provider: PimProviderId;
  readonly providerConfig: Record<string, string>;
  readonly domains?: readonly string[];
  readonly priority?: number;
}

function numberValue(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function optionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value !== '' ? value : undefined;
}

function pimAccount(value: unknown): PimAccount | null {
  if (value === null || typeof value !== 'object') return null;
  const raw = value as Record<string, unknown>;
  const id = stringValue(raw.id);
  if (id === '') return null;
  return {
    id,
    displayName: stringValue(raw.displayName),
    provider: stringValue(raw.provider),
    enabled: isTrue(raw.enabled),
  };
}

/** GET /api/connect/pim/accounts — the configured calendar accounts (no secrets). Rejects on a
 * non-200 (incl. 401/503 sidecar-unconfigured) so the connect section shows the offline/error note. */
export async function listPimAccounts(): Promise<PimAccountList> {
  const raw = await getJSON<{ accounts?: readonly unknown[] }>(GOV_PIM_ACCOUNTS_PATH);
  const accounts = (raw.accounts ?? []).flatMap((entry): PimAccount[] => {
    const acct = pimAccount(entry);
    return acct === null ? [] : [acct];
  });
  return { accounts };
}

/** POST /api/connect/pim/accounts — create an account with the operator's own provider credentials.
 * A duplicate id throws `Error("HTTP 409")`; a validation error throws `Error("HTTP 400")`. */
export function createPimAccount(body: PimCreateAccountRequest): Promise<PimAccount> {
  return postJSON<PimAccount>(GOV_PIM_ACCOUNTS_PATH, body);
}

/** DELETE /api/connect/pim/accounts/{id} — remove the account (the backend appends ?logout=true so
 * the linked session is dropped too). */
export async function deletePimAccount(id: string): Promise<void> {
  await deleteJSON<unknown>(`${GOV_PIM_ACCOUNTS_PATH}/${encodeURIComponent(id)}`);
}

/** GET …/{id}/google/start — mint the Google consent URL + the redirect URI the operator registers
 * in their Google Cloud client. A missing account throws `Error("HTTP 404")`; an account without a
 * clientId/secret throws `Error("HTTP 400")`. */
export function pimGoogleStart(id: string): Promise<PimGoogleStart> {
  return getJSON<PimGoogleStart>(`${GOV_PIM_ACCOUNTS_PATH}/${encodeURIComponent(id)}/google/start`);
}

/** POST …/{id}/logout — drop the linked session without deleting the account. */
export async function pimLogout(id: string): Promise<void> {
  await postJSON<unknown>(`${GOV_PIM_ACCOUNTS_PATH}/${encodeURIComponent(id)}/logout`);
}

/** POST …/{id}/auth/start — begin the Microsoft/Outlook device-code grant. The 200 carries the
 * userCode + verificationUrl the wizard renders. A missing account throws `Error("HTTP 404")`; a
 * provider that doesn't support device-code (e.g. google) throws `Error("HTTP 400")`. */
export async function pimDeviceStart(id: string): Promise<PimDeviceStart> {
  const raw = await postJSON<Record<string, unknown>>(
    `${GOV_PIM_ACCOUNTS_PATH}/${encodeURIComponent(id)}/auth/start`,
  );
  return {
    userCode: stringValue(raw.userCode),
    verificationUrl: stringValue(raw.verificationUrl),
    message: stringValue(raw.message),
    expiresIn: numberValue(raw.expiresIn),
  };
}

/** GET …/{id}/auth/status — poll the device-code flow state. */
export async function pimAuthStatus(id: string): Promise<PimAuthStatus> {
  const raw = await getJSON<Record<string, unknown>>(
    `${GOV_PIM_ACCOUNTS_PATH}/${encodeURIComponent(id)}/auth/status`,
  );
  return {
    status: stringValue(raw.status),
    message: stringValue(raw.message),
    userCode: optionalString(raw.userCode),
    verificationUrl: optionalString(raw.verificationUrl),
  };
}

/** POST …/{id}/auth/cancel — abort a pending device-code flow. */
export async function pimAuthCancel(id: string): Promise<void> {
  await postJSON<unknown>(`${GOV_PIM_ACCOUNTS_PATH}/${encodeURIComponent(id)}/auth/cancel`);
}

/** isCalendarServer detects the aura-pim-mcp calendar MCP server by recipe/source (preferred) with a
 * name/source-substring fallback (the catalog recipe id is `calendar`; the source may mention `pim`
 * or `aura-pim-mcp`), so the detail pane knows when to render the calendar connect section. Pure;
 * lives here (not the component file) so CalendarConnect.tsx exports only components. */
export function isCalendarServer(server: {
  readonly name: string;
  readonly source: string;
}): boolean {
  const source = server.source.toLowerCase();
  const name = server.name.toLowerCase();
  return (
    name === 'calendar' ||
    source === 'recipe:calendar' ||
    source.includes('pim') ||
    source.includes('aura-pim-mcp')
  );
}
