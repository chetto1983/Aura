import { getJSON, postJSON } from '../api/json';

// onboardingApi.ts is the Phase-28 data layer over the Plan-05 onboarding REST routes. It
// mirrors graphApi.ts / governanceApi.ts EXACTLY: ALWAYS credentials: 'same-origin' (the SPA is
// served by the same binary that exposes the routes, behind the Phase-24 RequireAuth whole-origin
// gate), Accept: application/json, and a NON-200 — INCLUDING 401 AND 403 — THROWS
// `Error("HTTP <n>")` rather than returning a discriminated union. The wizard drives these through
// explicit try/catch so an expired session (401) surfaces a VISIBLE auth-error state and a
// missing-capability (403) surfaces the no-permission copy, never a silent blank (T-28-06-04).
//
// The four routes (28-05-SUMMARY §Next Phase Readiness):
//   POST /api/onboarding/start                          → OnboardingStart
//   POST /api/onboarding/{token}/step                   → OnboardingStepResponse
//   POST /api/onboarding/{token}/provision              → OnboardingProvisionResponse
//   GET  /api/onboarding/{token}/telegram-status        → OnboardingTelegramStatus
//
// SECRETS: the initial password is sent ONLY on provision (in its own body, never stored) and is
// never read back from any response. The provision response carries the t.me/<bot>?start=<token>
// deep-link + a server-rendered QR SVG — never the bot token (T-28-06-01/02, Plan 05 guarantees).

export const ONBOARDING_START_PATH = '/api/onboarding/start';

/** The onboarding step intents the wizard drives (the closed set the backend validates). */
export type OnboardingIntent = 'answer' | 'confirm' | 'edit' | 'skip';

/** POST /start response: the opaque session token, the first step + prompt content + status, and
 * the D-06 capability picker options (the creator's grants, already '*'-excluded server-side). */
export interface OnboardingStart {
  readonly sessionToken: string;
  readonly step: string;
  readonly content: string;
  readonly status: string;
  readonly capabilityOptions: readonly string[];
}

/** The structured edit payload (an edit restates facts). Only the fields the wizard surfaces. */
export interface OnboardingAnswers {
  readonly name?: string;
  readonly role?: string;
  readonly company?: string;
  readonly location?: string;
  readonly lang?: string;
  readonly timezone?: string;
  readonly tonePreference?: string;
  readonly responseLength?: string;
}

/** POST /{token}/step body: an intent plus optional free text (answer) / structured edit. */
export interface OnboardingStepRequest {
  readonly intent: OnboardingIntent;
  readonly text?: string;
  readonly answers?: OnboardingAnswers;
}

/** POST /{token}/step response (D-03): {content, step, status, draft?, preferences?}. */
export interface OnboardingStepResponse {
  readonly content: string;
  readonly step: string;
  readonly status: string;
  readonly draft?: string;
  readonly preferences?: string;
}

/** POST /{token}/provision body: the new login email + write-only initial password, the requested
 * capability subset (re-validated server-side), and whether to mint a Telegram link. */
export interface OnboardingProvisionRequest {
  readonly email: string;
  readonly password: string;
  readonly securityQuestion: string;
  readonly securityAnswer: string;
  readonly capabilities: readonly string[];
  readonly linkTelegram: boolean;
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

function stepPath(token: string): string {
  return `/api/onboarding/${encodeURIComponent(token)}/step`;
}

function provisionPath(token: string): string {
  return `/api/onboarding/${encodeURIComponent(token)}/provision`;
}

function telegramStatusPath(token: string): string {
  return `/api/onboarding/${encodeURIComponent(token)}/telegram-status`;
}

/** POST /api/onboarding/start — mint a server-held session and read the first step + the D-06
 * capability options. A non-200 (incl. 401/403) throws so the wizard shows the auth/permission/
 * error state, never a blank. The capability gate (identity.create) yields a 403 here. */
export function startOnboarding(): Promise<OnboardingStart> {
  return postJSON<OnboardingStart>(ONBOARDING_START_PATH, {});
}

/** POST /api/onboarding/{token}/step — apply one interview intent (answer/confirm/edit/skip) and
 * parse {content, step, status, draft?, preferences?}. Non-200 (incl. 401) throws. */
export function stepOnboarding(
  token: string,
  req: OnboardingStepRequest,
): Promise<OnboardingStepResponse> {
  return postJSON<OnboardingStepResponse>(stepPath(token), req);
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
