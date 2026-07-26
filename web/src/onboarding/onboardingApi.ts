import { getJSON, postJSON } from '../api/json';

// onboardingApi.ts is the Phase-28 data layer over the onboarding REST routes. It mirrors
// graphApi.ts / governanceApi.ts EXACTLY: ALWAYS credentials: 'same-origin' (the SPA is served by
// the same binary that exposes the routes, behind the Phase-24 RequireAuth whole-origin gate),
// Accept: application/json, and a NON-200 — INCLUDING 401 AND 403 — THROWS `Error("HTTP <n>")`
// rather than returning a discriminated union. The surfaces drive these through explicit try/catch
// so an expired session (401) surfaces a VISIBLE auth-error state and a missing-capability (403)
// surfaces the no-permission copy, never a silent blank (T-28-06-04).
//
// The five routes (Amendment #95 — the interview's /profile/start + /{token}/step +
// /{token}/profile collapsed into ONE stateless POST carrying the whole seed form):
//   GET  /api/onboarding/status                          → OnboardingStatus
//   POST /api/onboarding/profile                         → OnboardingProfileComplete
//   POST /api/onboarding/start                           → OnboardingStart
//   POST /api/onboarding/{token}/provision               → OnboardingProvisionResponse
//   GET  /api/onboarding/{token}/telegram-status         → OnboardingTelegramStatus
//
// SECRETS: the initial password is sent ONLY on provision (in its own body, never stored) and is
// never read back from any response. The provision response carries the t.me/<bot>?start=<token>
// deep-link + a server-rendered QR SVG — never the bot token (T-28-06-01/02, Plan 05 guarantees).

export const ONBOARDING_START_PATH = '/api/onboarding/start';
export const ONBOARDING_PROFILE_PATH = '/api/onboarding/profile';

/** POST /start response: the opaque session token and the D-06 capability picker options (the
 * creator's grants, already '*'-excluded server-side). */
export interface OnboardingStart {
  readonly sessionToken: string;
  readonly capabilityOptions: readonly string[];
}

/** The typed first-run profile form (Amendment #95). ONE wire type serves both flows — the
 * self-service POST /api/onboarding/profile body and the `seed` object provisioning carries for
 * the identity it creates — so there is one validator and one mapper server-side. Every field is
 * optional; an entirely blank seed is a skip, DERIVED server-side (there is no `skip` flag). */
export interface OnboardingSeed {
  readonly name?: string;
  readonly lang?: string;
  readonly location?: string;
  readonly timezone?: string;
  readonly role?: string;
  readonly company?: string;
}

/** POST /{token}/provision body: the new login email + write-only initial password, the requested
 * capability subset (re-validated server-side), whether to mint a Telegram link, and the profile
 * seed written into the NEW identity's graph. */
export interface OnboardingProvisionRequest {
  readonly email: string;
  readonly password: string;
  readonly securityQuestion: string;
  readonly securityAnswer: string;
  readonly capabilities: readonly string[];
  readonly linkTelegram: boolean;
  readonly seed: OnboardingSeed;
}

/** POST /{token}/provision response: the new identity id and (when linkTelegram) the Telegram
 * deep-link + a server-rendered scannable QR SVG. The bot token is NEVER in either field. */
export interface OnboardingProvisionResponse {
  readonly identityId: string;
  readonly deepLink?: string;
  readonly qrSvg?: string;
}

/** GET /{token}/telegram-status response: whether the minted token was consumed (user scanned). */
export interface OnboardingTelegramStatus {
  readonly linked: boolean;
}

export interface OnboardingStatus {
  readonly required: boolean;
  readonly completed: boolean;
  readonly skipped: boolean;
}

/** POST /api/onboarding/profile response. `completed` and `skipped` are mutually exclusive.
 * `sessionToken` is the freshly minted Telegram poll session the completion screen feeds to
 * GET /api/onboarding/{sessionToken}/telegram-status, so no pre-mint round-trip is needed. */
export interface OnboardingProfileComplete {
  readonly completed: boolean;
  readonly skipped: boolean;
  readonly sessionToken: string;
  readonly deepLink?: string;
  readonly qrSvg?: string;
}

function provisionPath(token: string): string {
  return `/api/onboarding/${encodeURIComponent(token)}/provision`;
}

function telegramStatusPath(token: string): string {
  return `/api/onboarding/${encodeURIComponent(token)}/telegram-status`;
}

/** POST /api/onboarding/start — mint a server-held session and read the D-06 capability options.
 * A non-200 (incl. 401/403) throws so the wizard shows the auth/permission/error state, never a
 * blank. The capability gate (identity.create) yields a 403 here. */
export function startOnboarding(): Promise<OnboardingStart> {
  return postJSON<OnboardingStart>(ONBOARDING_START_PATH, {});
}

/** POST /api/onboarding/profile — submit the whole seed form in ONE stateless request. There is no
 * session token and no accumulated server state. A 502 (memory sidecar down, or Telegram not
 * configured so no link could be minted) throws so the surface renders its save-error retry. */
export function submitOnboardingProfile(seed: OnboardingSeed): Promise<OnboardingProfileComplete> {
  return postJSON<OnboardingProfileComplete>(ONBOARDING_PROFILE_PATH, seed);
}

/** POST /api/onboarding/{token}/provision — run the cross-store saga. The password is sent ONLY
 * here and never read back. Returns {identityId, deepLink?, qrSvg?}. A 403 (no capability), 409
 * (duplicate/empty email), or 502 (rolled-back saga) throws so the wizard renders the matching
 * distinct error copy (T-28-06-04). */
export function provisionOnboarding(
  token: string,
  req: OnboardingProvisionRequest,
): Promise<OnboardingProvisionResponse> {
  return postJSON<OnboardingProvisionResponse>(provisionPath(token), req);
}

/** GET /api/onboarding/{token}/telegram-status — the REST poll over PendingConsumed. Non-200
 * (incl. 401) throws; the caller stops polling on a thrown auth error. */
export function fetchTelegramStatus(token: string): Promise<OnboardingTelegramStatus> {
  return getJSON<OnboardingTelegramStatus>(telegramStatusPath(token));
}

export function fetchOnboardingStatus(): Promise<OnboardingStatus> {
  return getJSON<OnboardingStatus>('/api/onboarding/status');
}
