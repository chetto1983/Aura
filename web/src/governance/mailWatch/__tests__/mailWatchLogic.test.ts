import { afterEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import i18n from '../../../i18n/i18n';
import { HttpError } from '../../../api/json';
import { decision, label, rule } from '../../../test/mailWatchServer';
import {
  browserDay,
  canResume,
  canTakeRule,
  clockTime,
  dayAndTime,
  decisionKey,
  draftIsComplete,
  errorSentence,
  failingSince,
  failureSentence,
  groupDecisions,
  isTypingTarget,
  isUnavailable,
  longDay,
  parseRuleKey,
  pausedSentence,
  previewRule,
  ruleInputFrom,
  ruleOnFile,
  rowTime,
  sameRule,
  shortDay,
  tierCounts,
  triageAction,
  whySentence,
} from '../mailWatchLogic';

// The rules of the watch's cockpit that are not drawing. The preview mirrors DecideMail and
// parseRuleKey, so its table names the Go behaviour each row pins.

const t = i18n.t.bind(i18n) as TFunction;

afterEach(async () => {
  vi.restoreAllMocks();
  await i18n.changeLanguage('en');
});

describe('groupDecisions', () => {
  const client = label({ id: 'client', name: 'Client', position: 1 });
  const work = label({ id: 'work', name: 'Work', position: 0 });
  const ids = (rows: readonly { readonly item_id: string }[]) => rows.map((row) => row.item_id);

  it('puts now first whatever its label, then labels in order, then the unsorted', () => {
    const groups = groupDecisions(
      [
        decision({ item_id: 'plain' }),
        decision({ item_id: 'client', label_id: 'client' }),
        decision({ item_id: 'work', label_id: 'work' }),
        decision({ item_id: 'urgent', tier: 'now', label_id: 'client' }),
      ],
      [client, work],
    );
    expect(groups.map((group) => [group.kind, group.name, ids(group.rows)])).toEqual([
      ['now', '', ['urgent']],
      ['label', 'Work', ['work']],
      ['label', 'Client', ['client']],
      ['unsorted', '', ['plain']],
    ]);
  });

  it('drops a group with no row, so a quiet day shows only what it has', () => {
    const groups = groupDecisions([decision({ label_id: 'client' })], [client, work]);
    expect(groups.map((group) => group.id)).toEqual(['client']);
  });

  it('reads a label the person removed as no label', () => {
    const groups = groupDecisions([decision({ label_id: 'gone-label' })], [client]);
    expect(groups.map((group) => group.kind)).toEqual(['unsorted']);
  });

  it('keeps today above later inside a group, and the server order within a tier', () => {
    const [group] = groupDecisions(
      [
        decision({ item_id: 'later-1', tier: 'later' }),
        decision({ item_id: 'today-1' }),
        decision({ item_id: 'today-2' }),
      ],
      [],
    );
    expect(ids(group?.rows ?? [])).toEqual(['today-1', 'today-2', 'later-1']);
  });

  it('answers no group for an empty day', () => {
    expect(groupDecisions([], [client])).toEqual([]);
  });
});

it('tierCounts counts each tier', () => {
  expect(
    tierCounts([
      decision({ tier: 'now' }),
      decision({ tier: 'later' }),
      decision({ tier: 'later' }),
    ]),
  ).toEqual({ now: 1, today: 0, later: 2 });
});

it('decisionKey separates a mail and an event with the same id', () => {
  expect(decisionKey(decision({ item_id: 'x' }))).toBe('mail:x');
  expect(decisionKey(decision({ item_id: 'x', resource: 'calendar' }))).toBe('calendar:x');
});

describe('canTakeRule', () => {
  it.each([
    ['a readable mail', {}, true],
    ['an event, because rules do not apply to events', { resource: 'calendar' }, false],
    ['a gone item, which has no sender', { gone: true }, false],
    ['a failed sender, because a rule would beat that rung', { auth_failed: true }, false],
    ['a mail with no address', { from: '' }, false],
  ])('%s', (_name, fields, expected) => {
    expect(canTakeRule(decision(fields))).toBe(expected);
  });
});

describe('parseRuleKey mirrors the server', () => {
  it.each([
    ['Ada@Example.com', { key: 'ada@example.com', kind: 'sender' }],
    ['  ada@example.com  ', { key: 'ada@example.com', kind: 'sender' }],
    ['example.com', { key: 'example.com', kind: 'domain' }],
    ['@Example.com', { key: 'example.com', kind: 'domain' }],
    ['ada@', null],
    ['@@example.com', null],
    ['a b@example.com', null],
    ['nodot', null],
    ['exam ple.com', null],
    ['', null],
  ])('%s', (raw, expected) => {
    expect(parseRuleKey(raw)).toEqual(expected);
  });
});

describe('draftIsComplete', () => {
  it.each([
    [{ key: 'example.com', tier: 'now', labelId: '' }, true],
    [{ key: 'example.com', tier: '', labelId: 'l1' }, true],
    [{ key: 'example.com', tier: '', labelId: '' }, false],
    [{ key: 'nope', tier: 'now', labelId: '' }, false],
  ] as const)('%o', (draft, expected) => {
    expect(draftIsComplete(draft)).toBe(expected);
  });
});

describe('previewRule', () => {
  const sender = { key: 'ada@example.com', tier: 'later', labelId: '' } as const;
  const domain = { key: 'example.com', tier: 'later', labelId: '' } as const;

  it('counts the mail of a sender a rule would change', () => {
    const rows = [decision({ item_id: 'a' }), decision({ item_id: 'b', from: 'x@other.org' })];
    expect(previewRule(sender, rows, [], [])).toBe(1);
  });

  it('matches an address whatever its case', () => {
    expect(previewRule(sender, [decision({ from: 'ADA@Example.COM' })], [], [])).toBe(1);
  });

  it('matches a domain exactly, as the book does, and no subdomain', () => {
    const rows = [
      decision({ item_id: 'a', from: 'ada@example.com' }),
      decision({ item_id: 'b', from: 'bob@example.com' }),
      decision({ item_id: 'c', from: 'cy@mail.example.com' }),
    ];
    expect(previewRule(domain, rows, [], [])).toBe(2);
  });

  it('leaves out a sender with its own rule, because that rule beats a domain rule', () => {
    const rows = [decision({ from: 'ada@example.com' }), decision({ from: 'bob@example.com' })];
    expect(previewRule(domain, rows, [rule({ key: 'ada@example.com' })], [])).toBe(1);
  });

  it('does not let an existing domain rule hide a sender from a domain preview', () => {
    const onFile = rule({ key: 'example.com', key_kind: 'domain' });
    expect(previewRule(domain, [decision()], [onFile], [])).toBe(1);
  });

  it('a sender rule replaces the sender rule on file', () => {
    expect(previewRule(sender, [decision()], [rule({ tier: 'now' })], [])).toBe(1);
  });

  it.each([
    ['an event', { resource: 'calendar' }],
    ['a gone item', { gone: true }],
    ['a mail the person corrected', { decided_by: 'person' }],
    ['a mail already at that tier', { tier: 'later' }],
  ] as const)('leaves out %s', (_name, fields) => {
    expect(previewRule(sender, [decision(fields)], [], [])).toBe(0);
  });

  it('counts a mail failed authentication, because a rule wins over that rung', () => {
    expect(previewRule(sender, [decision({ auth_failed: true })], [], [])).toBe(1);
  });

  it('counts a label change on its own', () => {
    const draft = { key: 'ada@example.com', tier: '', labelId: 'l1' } as const;
    expect(previewRule(draft, [decision()], [], [label()])).toBe(1);
    expect(previewRule(draft, [decision({ label_id: 'l1' })], [], [label()])).toBe(0);
  });

  it("reads a label's tier in place of a rule's missing tier", () => {
    const draft = { key: 'ada@example.com', tier: '', labelId: 'l1' } as const;
    const labels = [label({ tier: 'later' })];
    expect(previewRule(draft, [decision({ tier: 'later', label_id: 'l1' })], [], labels)).toBe(0);
    expect(previewRule(draft, [decision({ tier: 'now', label_id: 'l1' })], [], labels)).toBe(1);
  });

  it('a rule with a discard label counts the rows it would discard', () => {
    const draft = { key: 'ada@example.com', tier: '', labelId: 'l1' } as const;
    const spam = label({ discard: true });
    const rows = [
      decision({ tier: 'later', label_id: 'l1' }),
      decision({ tier: 'later', label_id: 'l1', discarded: true }),
      decision({ tier: 'now', label_id: 'l1', discarded: true }),
    ];
    expect(previewRule(draft, rows, [], [spam])).toBe(2);
    expect(previewRule({ ...draft, tier: 'now' }, rows, [], [spam])).toBe(2);
    expect(previewRule(draft, rows, [], [label({ discard: true, tier: 'later' })])).toBe(1);
  });

  it('prefers the tier a rule states over its label tier', () => {
    const draft = { key: 'ada@example.com', tier: 'now', labelId: 'l1' } as const;
    const row = decision({ tier: 'now', label_id: 'l1' });
    expect(previewRule(draft, [row], [], [label({ tier: 'later' })])).toBe(0);
  });

  it.each([
    [{ key: 'nope', tier: 'now', labelId: '' }],
    [{ key: 'ada@example.com', tier: '', labelId: '' }],
  ] as const)('answers 0 for an incomplete draft %o', (draft) => {
    expect(previewRule(draft, [decision()], [], [])).toBe(0);
  });
});

describe('rule inputs', () => {
  it('ruleInputFrom takes the address, tier and label of a row', () => {
    expect(ruleInputFrom(decision({ tier: 'now', label_id: 'l1' }))).toEqual({
      key: 'ada@example.com',
      tier: 'now',
      label_id: 'l1',
    });
    expect(ruleInputFrom(decision())).toEqual({ key: 'ada@example.com', tier: 'today' });
  });

  it('sameRule compares what two rules do', () => {
    expect(sameRule(undefined, rule())).toBe(false);
    expect(sameRule(rule({ id: 'a' }), rule({ id: 'b' }))).toBe(true);
    expect(sameRule(rule({ tier: 'now' }), rule({ tier: 'later' }))).toBe(false);
    expect(sameRule(rule({ label_id: 'a' }), rule({ label_id: 'b' }))).toBe(false);
  });

  it('ruleOnFile finds a rule by key and kind', () => {
    const rules = [rule({ id: 'domain', key: 'x', key_kind: 'domain' }), rule({ id: 'sender' })];
    expect(ruleOnFile(rules, { key: 'ada@example.com', key_kind: 'sender' })?.id).toBe('sender');
    expect(ruleOnFile(rules, { key: 'x', key_kind: 'sender' })).toBeUndefined();
    expect(ruleOnFile(undefined, { key: 'x', key_kind: 'sender' })).toBeUndefined();
  });
});

describe('isTypingTarget', () => {
  function element(html: string): Element {
    const host = document.createElement('div');
    host.innerHTML = html;
    const first = host.firstElementChild;
    if (first === null) throw new Error(`nothing in ${html}`);
    return first;
  }

  it.each([
    ['<input />'],
    ['<textarea></textarea>'],
    ['<select></select>'],
    ['<div contenteditable="true"></div>'],
    ['<button role="combobox"></button>'],
    ['<div role="listbox"></div>'],
  ])('%s reads keys for itself', (html) => {
    expect(isTypingTarget(element(html))).toBe(true);
  });

  it('counts an element inside a field', () => {
    const inside = element('<div role="listbox"><span></span></div>').firstChild;
    expect(isTypingTarget(inside)).toBe(true);
  });

  it.each([['<button></button>'], ['<article tabindex="0"></article>']])('%s does not', (html) => {
    expect(isTypingTarget(element(html))).toBe(false);
  });

  it('is false for no target', () => {
    expect(isTypingTarget(null)).toBe(false);
  });
});

describe('triageAction', () => {
  const row = document.createElement('button');
  const inner = document.createElement('button');
  function press(key: string, fields: Partial<Parameters<typeof triageAction>[0]> = {}) {
    return triageAction({
      key,
      ctrlKey: false,
      metaKey: false,
      altKey: false,
      target: row,
      currentTarget: row,
      ...fields,
    });
  }

  it.each([
    ['1', { kind: 'tier', tier: 'now' }],
    ['2', { kind: 'tier', tier: 'today' }],
    ['3', { kind: 'tier', tier: 'later' }],
    ['j', { kind: 'move', delta: 1 }],
    ['J', { kind: 'move', delta: 1 }],
    ['k', { kind: 'move', delta: -1 }],
    ['K', { kind: 'move', delta: -1 }],
    ['ArrowDown', { kind: 'move', delta: 1 }],
    ['ArrowUp', { kind: 'move', delta: -1 }],
    ['l', { kind: 'label' }],
    ['L', { kind: 'label' }],
  ])('%s on a row', (key, expected) => {
    expect(press(key)).toEqual(expected);
  });

  it.each([['4'], ['x'], ['Enter'], [' '], ['Tab']])('%s means nothing', (key) => {
    expect(press(key)).toBeNull();
  });

  it.each([['ctrlKey'], ['metaKey'], ['altKey']])('leaves a chord with %s alone', (modifier) => {
    expect(press('1', { [modifier]: true })).toBeNull();
    expect(press('j', { [modifier]: true })).toBeNull();
  });

  it('keeps the arrows for the row itself, and the letters for anywhere in it', () => {
    expect(press('ArrowDown', { target: inner })).toBeNull();
    expect(press('ArrowUp', { target: inner })).toBeNull();
    expect(press('j', { target: inner })).toEqual({ kind: 'move', delta: 1 });
    expect(press('1', { target: inner })).toEqual({ kind: 'tier', tier: 'now' });
  });
});

describe('sentences from fixed codes', () => {
  it.each([
    ['rule', 'One of your rules decided this'],
    ['auth_failed', 'The sender failed authentication, so it never alerts'],
    ['people', 'You have written to this person before'],
    ['category', 'The provider filed it under promotions or social'],
    ['default', 'Nothing stood out, so it waits for the digest'],
    ['calendar', 'A change in your calendar'],
    ['person', 'You decided this'],
  ])('why: %s', (code, sentence) => {
    expect(whySentence(code, t)).toBe(sentence);
  });

  it('says why in Italian too', async () => {
    await i18n.changeLanguage('it');
    expect(whySentence('people', t)).toBe('Hai già scritto a questa persona');
  });

  it('falls back on the default reason for a code it does not know', () => {
    expect(whySentence('something_new', t)).toBe('Nothing stood out, so it waits for the digest');
  });

  it.each([
    ['provider_auth', /no longer sign in/],
    ['permission_denied', /refuses this permission/],
    ['account_not_found', /no longer exists/],
    ['invalid_cursor', /off and on again/],
    ['invalid_account', /refused this account/],
    ['invalid_resource', /refused the request/],
    ['invalid_items', /refused the list/],
    ['too_many_items', /Too many items/],
    ['provider_transient', /next check/],
    ['unsupported_resource', /does not offer/],
    ['pim_empty_cursor', /where to read from/],
    ['pim_not_authorized', /Reconnect it/],
    ['pim_token_unavailable', /access to this account/],
    ['pim_unreachable', /cannot be reached/],
    ['pim_read_failed', /halfway/],
    ['pim_bad_response', /cannot read/],
    ['pim_call_failed', /did not go through/],
    ['pim_http_503', /status 503/],
    ['never_seen_before', /reported a problem/],
    ['', /reported a problem/],
  ])('failure: %s', (code, sentence) => {
    expect(failureSentence(code, t)).toMatch(sentence);
  });

  it('translates a failure', async () => {
    await i18n.changeLanguage('it');
    expect(failureSentence('invalid_cursor', t)).toBe(
      'Aura ha perso il segno. Spegni e riaccendi il controllo.',
    );
    expect(failureSentence('pim_http_500', t)).toMatch(/stato 500/);
    expect(failureSentence('pim_not_authorized', t)).toMatch(/Ricollegalo/);
  });

  it.each([
    'pim_empty_cursor',
    'pim_not_authorized',
    'pim_token_unavailable',
    'pim_unreachable',
    'pim_read_failed',
    'pim_bad_response',
    'pim_call_failed',
  ])('%s has its own sentence in Italian too', async (code) => {
    await i18n.changeLanguage('it');
    expect(failureSentence(code, t)).not.toBe(failureSentence('never_seen_before', t));
  });

  it.each([
    ['operator', 'Paused by hand'],
    ['failures', 'Paused after repeated failures'],
    ['identity_deactivated', 'Paused because this identity was deactivated'],
    ['something_new', 'Paused'],
    [undefined, 'Paused'],
  ])('paused: %s', (reason, sentence) => {
    expect(pausedSentence(reason, t)).toBe(sentence);
  });

  it('a deactivation is the one pause a person cannot lift', () => {
    expect(canResume('identity_deactivated')).toBe(false);
    expect(canResume('failures')).toBe(true);
    expect(canResume('operator')).toBe(true);
    expect(canResume(undefined)).toBe(true);
  });
});

describe('errorSentence', () => {
  it.each([
    ['account_required', /Choose an account/],
    ['invalid_day', /not a day/],
    ['empty_correction', /tier or a label/],
    ['invalid_tier', /not a tier/],
    ['unknown_label', /no longer exists/],
    ['empty_rule', /tier, a label or both/],
    ['invalid_address', /not a whole address/],
    ['invalid_rule_key', /whole address, or a domain/],
    ['invalid_label_name', /1 to 60/],
    ['label_exists', /already exists/],
    ['invalid_digest_hour', /0 to 23/],
    ['invalid_alert_cap', /0 to 100/],
    ['a_code_from_a_newer_server', /cannot be saved as it is/],
  ])('translates the refusal %s', async (code, sentence) => {
    expect(errorSentence(new HttpError(400, code), t)).toMatch(sentence);
    await i18n.changeLanguage('it');
    const italian = errorSentence(new HttpError(400, code), t);
    await i18n.changeLanguage('en');
    expect(italian).not.toBe(errorSentence(new HttpError(400, code), t));
  });

  it('translates a 502, which carries a fixed PIM code', () => {
    expect(errorSentence(new HttpError(502, 'provider_auth'), t)).toMatch(/no longer sign in/);
  });

  it('says a 404 plainly', () => {
    expect(errorSentence(new HttpError(404, 'not_found'), t)).toBe('That is no longer there.');
  });

  it('says a deployment cannot watch', () => {
    expect(errorSentence(new HttpError(503, 'mail_watch_unavailable'), t)).toMatch(/not available/);
  });

  it.each([
    ['a 400 with no sentence', new HttpError(400)],
    ['a 502 with no code', new HttpError(502)],
    ['a 500', new HttpError(500, 'mail_watch_failed')],
    ['a transport failure', new TypeError('network')],
  ])('falls back on %s', (_name, error) => {
    expect(errorSentence(error, t)).toBe('Something went wrong. Try again.');
  });

  it('isUnavailable reads the code, not the status', () => {
    expect(isUnavailable(new HttpError(503, 'mail_watch_unavailable'))).toBe(true);
    expect(isUnavailable(new HttpError(503))).toBe(false);
    expect(isUnavailable(new Error('mail_watch_unavailable'))).toBe(false);
  });
});

describe('times', () => {
  it('browserDay is the local calendar day, padded', () => {
    expect(browserDay(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05');
    expect(browserDay(new Date(2026, 10, 21, 0, 1))).toBe('2026-11-21');
  });

  it('longDay and shortDay read the day itself', () => {
    expect(longDay('2026-11-01', 'en-GB')).toBe('Sunday 1 November');
    expect(longDay('2026-11-01', 'it')).toBe('domenica 1 novembre');
    expect(shortDay('2026-10-28', 'en-GB')).toBe('28 October');
    expect(shortDay('2026-10-28', 'it')).toBe('28 ottobre');
  });

  it('a mail shows when it was decided, because it has no start', () => {
    const row = decision({ start: null, decided_at: '2026-11-01T09:12:00Z' });
    expect(rowTime(row, 'en-GB')).toBe(clockTime('2026-11-01T09:12:00Z', 'en-GB'));
  });

  it('a gone item shows when it was decided too', () => {
    const row = decision({ gone: true, start: null });
    expect(rowTime(row, 'en-GB')).toBe(clockTime(row.decided_at, 'en-GB'));
  });

  it('an event shows its own date and time', () => {
    const row = decision({ resource: 'calendar', start: '2026-11-03T14:30:00Z' });
    expect(rowTime(row, 'en-GB')).toBe(dayAndTime('2026-11-03T14:30:00Z', 'en-GB'));
    expect(rowTime(row, 'en-GB')).toMatch(/Nov/);
  });

  it('an all-day event shows the date, with no time and in no zone', () => {
    const format = vi.spyOn(Intl, 'DateTimeFormat');
    const row = decision({ resource: 'calendar', start: '2026-11-03T00:00:00Z', all_day: true });
    expect(rowTime(row, 'en-GB')).toBe('3 Nov');
    expect(format).toHaveBeenCalledWith('en-GB', {
      day: 'numeric',
      month: 'short',
      timeZone: 'UTC',
    });
  });

  it('a failure of today says its time, and an older one its date', () => {
    const now = new Date(2026, 10, 1, 12, 0);
    const today = new Date(2026, 10, 1, 7, 40).toISOString();
    const older = new Date(2026, 9, 30, 7, 40).toISOString();
    expect(failingSince(today, now, 'en-GB', t)).toBe(`since ${clockTime(today, 'en-GB')}`);
    expect(failingSince(older, now, 'en-GB', t)).toBe('since 30 Oct');
  });
});
