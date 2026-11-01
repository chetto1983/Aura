// mailWatchApi.ts is the cockpit's data layer for the mail and calendar watch (pim_watch_api.go).
// Every route sits behind the session and governance.write, and the shapes keep the wire's names.
// Each mutation carries the Idempotency-Key the shared fetch wrapper attaches (api/idempotency.ts),
// as pimApi.ts's do. Every call goes through sendJSON rather than getJSON, because a read fails
// with a code worth translating too: a 502 names the PIM's fixed failure in `{error}`.
//
// `from`, `from_name` and `subject` on a decision are a stranger's text. They are data for React
// to render as text, and never reach markdown or an href.

import { sendJSON } from '../api/json';

const ROUTE = '/api/mail-watch';

export const MAIL_WATCH_UNAVAILABLE = 'mail_watch_unavailable';

export const MAIL_WATCH_TIERS = ['now', 'today', 'later'] as const;
export type MailWatchTier = (typeof MAIL_WATCH_TIERS)[number];

export interface MailWatchSettings {
  readonly digest_hour: number;
  readonly daily_alert_cap: number;
  readonly interval_minutes: number;
  readonly last_digest_at: string | null;
}

export interface MailWatchResource {
  readonly resource: string;
  readonly status: 'ok' | 'failing' | 'unsupported';
  /** A fixed code for the UI to translate: a named one, or pim_http_<status>. */
  readonly last_error?: string;
  readonly failing_since: string | null;
  readonly advanced_at: string | null;
}

export interface MailWatchAccount {
  readonly account_id: string;
  readonly paused: boolean;
  readonly paused_reason?: string;
  readonly next_run_at: string | null;
  readonly alerts_today: number;
  readonly resources: readonly MailWatchResource[];
}

export interface MailWatchStatus {
  readonly configured: boolean;
  readonly reason?: string;
  readonly settings: MailWatchSettings;
  /** The accounts with a live watch, paused ones included. An account not listed is not watched. */
  readonly accounts: readonly MailWatchAccount[];
}

export interface MailWatchDecision {
  readonly account_id: string;
  readonly resource: string;
  readonly item_id: string;
  readonly calendar_id?: string;
  readonly from: string;
  readonly from_name: string;
  readonly subject: string;
  /** An event's start. A mail has none, and with all_day this is the date at midnight UTC. */
  readonly start: string | null;
  readonly all_day?: boolean;
  readonly gone: boolean;
  readonly auth_failed: boolean;
  readonly tier: MailWatchTier;
  /** What happened to an event. A mail has none. */
  readonly change?: 'new' | 'moved' | 'cancelled';
  readonly label_id?: string;
  readonly label?: string;
  readonly decided_by: string;
  readonly decided_at: string;
}

export interface MailWatchDay {
  /** YYYY-MM-DD in the identity's zone. */
  readonly day: string;
  readonly decisions: readonly MailWatchDecision[];
}

export interface MailWatchRule {
  readonly id: string;
  readonly key: string;
  readonly key_kind: 'sender' | 'domain';
  readonly tier?: MailWatchTier;
  readonly label_id?: string;
  readonly source: 'person' | 'correction';
  readonly created_at: string;
}

export interface MailWatchLabel {
  readonly id: string;
  readonly name: string;
  readonly tier?: MailWatchTier;
  readonly position: number;
}

export interface MailWatchCorrection {
  readonly resource: string;
  readonly item_id: string;
  readonly tier?: MailWatchTier;
  /** Left out, it keeps the label. An empty string clears it. */
  readonly label_id?: string;
}

export interface MailWatchCorrected {
  readonly tier: MailWatchTier;
  readonly label_id?: string;
  /** Set when this correction taught a rule. */
  readonly rule?: MailWatchRule;
  /** The rule is new. One merged into a rule on file holds the person's earlier words. */
  readonly rule_created?: boolean;
}

export interface MailWatchRuleInput {
  readonly key: string;
  readonly tier?: MailWatchTier;
  readonly label_id?: string;
}

export interface MailWatchLabelInput {
  readonly name: string;
  readonly tier?: MailWatchTier;
}

function accountPath(accountId: string): string {
  return `${ROUTE}/accounts/${encodeURIComponent(accountId)}`;
}

export function getMailWatch(): Promise<MailWatchStatus> {
  return sendJSON<MailWatchStatus>('GET', ROUTE);
}

export function saveMailWatchSettings(input: {
  readonly digest_hour: number;
  readonly daily_alert_cap: number;
}): Promise<MailWatchSettings> {
  return sendJSON<MailWatchSettings>('PUT', `${ROUTE}/settings`, input);
}

export async function enableMailWatch(accountId: string): Promise<void> {
  await sendJSON<unknown>('POST', `${accountPath(accountId)}/enable`);
}

export async function disableMailWatch(accountId: string): Promise<void> {
  await sendJSON<unknown>('POST', `${accountPath(accountId)}/disable`);
}

/** A day of decisions, newest first. A null day is today in the identity's zone, which only the
 * server knows, so the browser's date is never sent for it and the answer names the day read. */
export function getDecisions(accountId: string, day: string | null): Promise<MailWatchDay> {
  const query = day === null ? '' : `?day=${encodeURIComponent(day)}`;
  return sendJSON<MailWatchDay>('GET', `${accountPath(accountId)}/decisions${query}`);
}

export function correctDecision(
  accountId: string,
  correction: MailWatchCorrection,
): Promise<MailWatchCorrected> {
  return sendJSON<MailWatchCorrected>(
    'POST',
    `${accountPath(accountId)}/decisions/correct`,
    correction,
  );
}

export function listRules(): Promise<readonly MailWatchRule[]> {
  return sendJSON<readonly MailWatchRule[]>('GET', `${ROUTE}/rules`);
}

export function addRule(input: MailWatchRuleInput): Promise<MailWatchRule> {
  return sendJSON<MailWatchRule>('POST', `${ROUTE}/rules`, input);
}

export async function removeRule(id: string): Promise<void> {
  await sendJSON<unknown>('DELETE', `${ROUTE}/rules/${encodeURIComponent(id)}`);
}

export function listLabels(): Promise<readonly MailWatchLabel[]> {
  return sendJSON<readonly MailWatchLabel[]>('GET', `${ROUTE}/labels`);
}

export function addLabel(input: MailWatchLabelInput): Promise<MailWatchLabel> {
  return sendJSON<MailWatchLabel>('POST', `${ROUTE}/labels`, input);
}

export function updateLabel(id: string, input: MailWatchLabelInput): Promise<MailWatchLabel> {
  return sendJSON<MailWatchLabel>('PUT', `${ROUTE}/labels/${encodeURIComponent(id)}`, input);
}

export async function removeLabel(id: string): Promise<void> {
  await sendJSON<unknown>('DELETE', `${ROUTE}/labels/${encodeURIComponent(id)}`);
}
