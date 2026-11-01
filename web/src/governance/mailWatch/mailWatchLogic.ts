import type { TFunction } from 'i18next';
import { HttpError } from '../../api/json';
import {
  MAIL_WATCH_UNAVAILABLE,
  type MailWatchDecision,
  type MailWatchLabel,
  type MailWatchRule,
  type MailWatchRuleInput,
  type MailWatchTier,
} from '../mailWatchApi';

// mailWatchLogic.ts holds every rule of the mail watch's cockpit that is not drawing: how a day
// groups, what a rule would have changed, which key does what, and which sentence a fixed code
// reads as. It mirrors the server where the server decides, so each mirrored rule names its Go
// source: drift there makes the preview lie.

/** A tier a person may leave unset, on a rule or a label. */
export type TierChoice = MailWatchTier | '';

export interface DecisionGroup {
  readonly id: string;
  readonly kind: 'now' | 'label' | 'unsorted';
  /** The label's name; empty on the two groups the cockpit names itself. */
  readonly name: string;
  readonly rows: readonly MailWatchDecision[];
}

const TIER_RANK: Readonly<Record<MailWatchTier, number>> = { now: 0, today: 1, later: 2 };

/** `now` first, then the labels in the person's order, then "Da smistare". A group with no row
 * disappears, and a label the verdict still names but the person removed reads as no label. */
export function groupDecisions(
  decisions: readonly MailWatchDecision[],
  labels: readonly MailWatchLabel[],
): readonly DecisionGroup[] {
  const ordered = decisions.toSorted((a, b) => TIER_RANK[a.tier] - TIER_RANK[b.tier]);
  const rest = ordered.filter((row) => row.tier !== 'now');
  const known = new Set(labels.map((label) => label.id));
  const groups: readonly DecisionGroup[] = [
    { id: 'now', kind: 'now', name: '', rows: ordered.filter((row) => row.tier === 'now') },
    ...labels
      .toSorted((a, b) => a.position - b.position)
      .map((label): DecisionGroup => ({
        id: label.id,
        kind: 'label',
        name: label.name,
        rows: rest.filter((row) => row.label_id === label.id),
      })),
    {
      id: 'unsorted',
      kind: 'unsorted',
      name: '',
      rows: rest.filter((row) => row.label_id === undefined || !known.has(row.label_id)),
    },
  ];
  return groups.filter((group) => group.rows.length > 0);
}

export function tierCounts(
  decisions: readonly MailWatchDecision[],
): Readonly<Record<MailWatchTier, number>> {
  const count = (tier: MailWatchTier) => decisions.filter((row) => row.tier === tier).length;
  return { now: count('now'), today: count('today'), later: count('later') };
}

export function decisionKey(row: MailWatchDecision): string {
  return `${row.resource}:${row.item_id}`;
}

/** Only mail teaches or takes a rule, and only mail that can still be read: spec §5.3 keeps rules
 * off events, and a gone item has no sender. A mail that failed authentication is left out too,
 * because a rule beats that rung, so a rule made from it would let a forged sender reach `now`. */
export function canTakeRule(row: MailWatchDecision): boolean {
  return row.resource === 'mail' && !row.gone && !row.auth_failed && row.from !== '';
}

export interface ParsedRuleKey {
  readonly key: string;
  readonly kind: 'sender' | 'domain';
}

/** parseRuleKey in internal/pimwatch/service_rules.go, less the address grammar: anything this
 * passes and net/mail refuses comes back as the server's 400. */
export function parseRuleKey(raw: string): ParsedRuleKey | null {
  const key = raw.trim().toLowerCase().replace(/^@/, '');
  if (key.includes('@')) {
    return /^[^\s@]+@[^\s@]+$/.test(key) ? { key, kind: 'sender' } : null;
  }
  if (!key.includes('.') || /\s/.test(key)) return null;
  return { key, kind: 'domain' };
}

export interface RuleDraft {
  readonly key: string;
  readonly tier: TierChoice;
  readonly labelId: string;
}

export function draftIsComplete(draft: RuleDraft): boolean {
  return parseRuleKey(draft.key) !== null && (draft.tier !== '' || draft.labelId !== '');
}

function domainOf(address: string): string {
  return address.slice(address.indexOf('@') + 1);
}

/** How many of these decisions a rule would have changed, read the way DecideMail reads a rule in
 * internal/pimwatch/ladder.go: a sender rule beats a domain rule outright, a label's tier stands
 * in for a rule's missing tier, and an event is never matched. A mail the person corrected is
 * left out, because their word on that one mail stands whatever the rule says. */
export function previewRule(
  draft: RuleDraft,
  decisions: readonly MailWatchDecision[],
  rules: readonly MailWatchRule[],
  labels: readonly MailWatchLabel[],
): number {
  const parsed = parseRuleKey(draft.key);
  if (parsed === null || !draftIsComplete(draft)) return 0;
  const labelTier = labels.find((label) => label.id === draft.labelId)?.tier;
  const tier = draft.tier === '' ? labelTier : draft.tier;
  const senderRules = new Set(
    rules.filter((rule) => rule.key_kind === 'sender').map((rule) => rule.key),
  );
  return decisions.filter((row) => {
    if (row.resource !== 'mail' || row.gone || row.decided_by === 'person') return false;
    const address = row.from.toLowerCase();
    const matches =
      parsed.kind === 'sender'
        ? address === parsed.key
        : domainOf(address) === parsed.key && !senderRules.has(address);
    if (!matches) return false;
    const changesTier = tier !== undefined && tier !== row.tier;
    const changesLabel = draft.labelId !== '' && draft.labelId !== row.label_id;
    return changesTier || changesLabel;
  }).length;
}

/** The rule this decision would become, for "from now on, always like this". */
export function ruleInputFrom(row: MailWatchDecision): MailWatchRuleInput {
  return {
    key: row.from,
    tier: row.tier,
    ...(row.label_id === undefined ? {} : { label_id: row.label_id }),
  };
}

export function sameRule(a: MailWatchRule | undefined, b: MailWatchRule): boolean {
  return a !== undefined && a.tier === b.tier && a.label_id === b.label_id;
}

/** The rule already on file for the same key, which a write merges into or replaces. */
export function ruleOnFile(
  rules: readonly MailWatchRule[] | undefined,
  rule: Pick<MailWatchRule, 'key' | 'key_kind'>,
): MailWatchRule | undefined {
  return rules?.find((entry) => entry.key === rule.key && entry.key_kind === rule.key_kind);
}

export type TriageAction =
  | { readonly kind: 'move'; readonly delta: 1 | -1 }
  | { readonly kind: 'tier'; readonly tier: MailWatchTier }
  | { readonly kind: 'label' };

const TIER_KEYS: Readonly<Record<string, MailWatchTier>> = {
  '1': 'now',
  '2': 'today',
  '3': 'later',
};

/** A field that reads keys for itself, so the day must not take the focus from it on arrival. */
export function isTypingTarget(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    target.closest(
      'input, textarea, select, [contenteditable="true"], [role="combobox"], [role="listbox"]',
    ) !== null
  );
}

/** What a key means on a row's button or its tier control, or null for one that is not ours. No
 * field sits under either, so a key typed in the date input or a label menu never arrives here.
 * The arrows count only on the button, because inside the tier control they walk the tiers. */
export function triageAction(event: {
  readonly key: string;
  readonly ctrlKey: boolean;
  readonly metaKey: boolean;
  readonly altKey: boolean;
  readonly target: EventTarget | null;
  readonly currentTarget: EventTarget | null;
}): TriageAction | null {
  if (event.ctrlKey || event.metaKey || event.altKey) return null;
  const onRow = event.target === event.currentTarget;
  const key = event.key.toLowerCase();
  if (key === 'j' || (onRow && key === 'arrowdown')) return { kind: 'move', delta: 1 };
  if (key === 'k' || (onRow && key === 'arrowup')) return { kind: 'move', delta: -1 };
  if (key === 'l') return { kind: 'label' };
  const tier = TIER_KEYS[key];
  return tier === undefined ? null : { kind: 'tier', tier };
}

const WHY = 'governance.mailWatch.why';

/** `decided_by` as a plain sentence: the reason is a deterministic fact, so it is always said. */
export function whySentence(decidedBy: string, t: TFunction): string {
  return t(`${WHY}.${decidedBy}`, { defaultValue: t(`${WHY}.default`) });
}

const FAILURE = 'governance.mailWatch.failure';

/** A fixed PIM code as a sentence, named ones first and `pim_http_<status>` by its status. */
export function failureSentence(code: string, t: TFunction): string {
  const http = /^pim_http_(\d+)$/.exec(code);
  if (http !== null) return t(`${FAILURE}.http`, { status: http[1] });
  return t(`${FAILURE}.${code}`, { defaultValue: t(`${FAILURE}.unknown`) });
}

const PAUSED = 'governance.mailWatch.paused';

export function pausedSentence(reason: string | undefined, t: TFunction): string {
  return t(`${PAUSED}.${reason ?? 'unknown'}`, { defaultValue: t(`${PAUSED}.unknown`) });
}

/** A person cannot resume what their own deactivation stopped. */
export function canResume(reason: string | undefined): boolean {
  return reason !== 'identity_deactivated';
}

export function isUnavailable(error: unknown): boolean {
  return error instanceof HttpError && error.reason === MAIL_WATCH_UNAVAILABLE;
}

/** Why a deployment cannot watch, as a sentence. A code this build does not know adds nothing. */
export function unavailableReason(reason: string | undefined, t: TFunction): string {
  return t(`governance.mailWatch.overview.reason.${reason ?? 'none'}`, { defaultValue: '' });
}

const INVALID = 'governance.mailWatch.invalid';

/** A failed call as the sentence the person reads. Every status answers a fixed code in `{error}`,
 * and a 400's says what to fix; a code this build does not know reads as one generic sentence. */
export function errorSentence(error: unknown, t: TFunction): string {
  if (error instanceof HttpError) {
    if (error.status === 400 && error.reason !== '') {
      return t(`${INVALID}.${error.reason}`, { defaultValue: t(`${INVALID}.unknown`) });
    }
    if (error.status === 502 && error.reason !== '') return failureSentence(error.reason, t);
    if (error.status === 404) return t('governance.mailWatch.errors.notFound');
    if (isUnavailable(error)) return t('governance.mailWatch.errors.unavailable');
  }
  return t('governance.mailWatch.errors.failed');
}

/** The browser's calendar day, as the date input reads it. */
export function browserDay(now: Date): string {
  const month = String(now.getMonth() + 1).padStart(2, '0');
  const day = String(now.getDate()).padStart(2, '0');
  return `${String(now.getFullYear())}-${month}-${day}`;
}

/** How long a resource has been failing. A clock time alone would read as today's, so an older
 * failure says its date. */
export function failingSince(iso: string, now: Date, locale: string, t: TFunction): string {
  const since = new Date(iso);
  if (browserDay(since) === browserDay(now)) {
    return t('governance.mailWatch.state.sinceTime', { time: clockTime(iso, locale) });
  }
  return t('governance.mailWatch.state.sinceDay', { date: dayMonth(since, locale) });
}

function dayMonth(date: Date, locale: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(locale, {
    day: 'numeric',
    month: 'short',
    ...(timeZone === undefined ? {} : { timeZone }),
  }).format(date);
}

/** A YYYY-MM-DD as words. It reads the date at noon UTC in UTC, so no zone can move the day. */
export function longDay(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    timeZone: 'UTC',
  }).format(new Date(`${day}T12:00:00Z`));
}

export function shortDay(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    day: 'numeric',
    month: 'long',
    timeZone: 'UTC',
  }).format(new Date(`${day}T12:00:00Z`));
}

export function clockTime(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit' }).format(
    new Date(iso),
  );
}

export function dayAndTime(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(iso));
}

/** The time a row shows. A mail has no `start`, so it shows when Aura decided, which is within a
 * tick of its arrival. An all-day event's `start` is a date at midnight UTC: it shows the date in
 * no zone and no time. */
export function rowTime(row: MailWatchDecision, locale: string): string {
  if (row.start === null) return clockTime(row.decided_at, locale);
  if (row.all_day === true) return dayMonth(new Date(row.start), locale, 'UTC');
  return dayAndTime(row.start, locale);
}
