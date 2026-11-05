import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import i18n from '../../../i18n/i18n';
import {
  activeRow,
  CORRECT,
  DECISIONS,
  dateInput,
  itemAround,
  openDay,
  press,
  rowOf,
  theDay,
  THREE,
} from '../../../test/mailWatchDay';
import {
  decision,
  hang,
  label,
  mailWatchServer,
  rule,
  settled,
  watchDay,
} from '../../../test/mailWatchServer';

// The day of one account, through the real dialog, hooks and API module: what a person reads. The
// keys are in DecisionKeys.test.tsx, and what a correction teaches in DecisionCorrections.test.tsx.

let server: ReturnType<typeof mailWatchServer>;

beforeEach(() => {
  server = mailWatchServer();
});
afterEach(async () => {
  vi.unstubAllGlobals();
  // Before the language goes back, or the rows still mounted re-render outside act.
  cleanup();
  await i18n.changeLanguage('en');
});

describe('the states of a day', () => {
  it('announces the wait while the PIM is read', async () => {
    server.on(DECISIONS, hang);
    openDay();
    expect(await screen.findByText('Reading the day from your mail…')).toBeTruthy();
    expect(screen.getByText('Reading the day from your mail…').getAttribute('role')).toBe('status');
  });

  it('says what went wrong, and tries again', async () => {
    let attempts = 0;
    server.on(DECISIONS, () => {
      attempts += 1;
      return attempts === 1
        ? { status: 502, body: { error: 'provider_transient' } }
        : { body: watchDay([decision()]) };
    });
    openDay();
    expect((await screen.findByRole('alert')).textContent).toMatch(/provider is having trouble/);
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Quarterly numbers')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('is empty only when the whole day is', async () => {
    theDay(server);
    openDay();
    expect(await screen.findByText('Nothing on this day')).toBeTruthy();
    expect(document.querySelector('[data-mail-watch-row]')).toBeNull();
    expect(screen.queryByRole('img')).toBeNull();
  });

  it('says when the server cut the day short', async () => {
    const rows = Array.from({ length: 200 }, (_, n) => decision({ item_id: `m${String(n)}` }));
    theDay(server, ...rows);
    openDay();
    expect(await screen.findByText('Showing the latest 200 items of this day.')).toBeTruthy();
  });

  it('asks for today without a date, then for the day chosen', async () => {
    theDay(server, decision());
    openDay();
    await screen.findByText('Quarterly numbers');
    fireEvent.change(dateInput(), { target: { value: '2026-10-28' } });
    await waitFor(() => {
      const days = server.writes('GET', '/api/mail-watch/accounts/work/decisions');
      expect(days.map((call) => call.query)).toEqual(['', '?day=2026-10-28']);
    });
  });

  it('goes back to today when the date is cleared', async () => {
    theDay(server, decision());
    openDay();
    await screen.findByText('Quarterly numbers');
    const input = dateInput();
    fireEvent.change(input, { target: { value: '2026-10-28' } });
    fireEvent.change(input, { target: { value: '' } });
    await waitFor(() => {
      expect(screen.getByText('Quarterly numbers')).toBeTruthy();
    });
  });

  it('heads the day and shows its shape', async () => {
    theDay(server, ...THREE);
    openDay();
    expect(await screen.findByRole('img', { name: '1 now, 2 today, 0 later' })).toBeTruthy();
    expect(screen.getByRole('heading', { level: 3 }).textContent).not.toBe('');
  });

  it('heads the day the server answered, not the browser’s', async () => {
    server.on(DECISIONS, () => ({ body: watchDay([decision()], '2026-03-15') }));
    openDay();
    expect((await screen.findByRole('heading', { level: 3 })).textContent).toBe('Sunday, March 15');
    expect(dateInput()).toHaveProperty('value', '2026-03-15');
  });

  it('names no day until the server does', async () => {
    server.on(DECISIONS, hang);
    openDay();
    await screen.findByText('Reading the day from your mail…');
    expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
    expect(dateInput()).toHaveProperty('value', '');
  });
});

describe('the discarded mail', () => {
  it('shows the discarded mail under its toggle', async () => {
    server.on(DECISIONS, (call) => ({
      body: call.query.includes('discarded=1')
        ? watchDay([decision({ subject: 'Junk', tier: 'later', discarded: true })], '2026-11-01', 2)
        : watchDay([decision({ subject: 'Kept' })], '2026-11-01', 2),
    }));
    openDay();
    await screen.findByText('Kept');
    expect(screen.queryByText('Junk')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Discarded (2)' }));
    expect(await screen.findByText('Junk')).toBeTruthy();
    expect(screen.queryByText('Kept')).toBeNull();
    expect(screen.getByText('Discarded')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Discarded (2)' }).getAttribute('aria-pressed')).toBe(
      'true',
    );
  });

  it('has no toggle on a day with none', async () => {
    theDay(server, decision({ subject: 'Kept' }));
    openDay();
    await screen.findByText('Kept');
    expect(screen.queryByRole('button', { name: /^Discarded/ })).toBeNull();
  });
});

describe('the groups', () => {
  it('runs now, then labels, then the mail with no label, and hides what is empty', async () => {
    server.on('GET /api/mail-watch/labels', () => ({
      body: [
        label({ id: 'l1', name: 'Client', position: 0 }),
        label({ id: 'l2', name: 'Empty one', position: 1 }),
      ],
    }));
    theDay(
      server,
      decision({ item_id: 'c', subject: 'Plain' }),
      decision({ item_id: 'b', subject: 'Labelled', label_id: 'l1' }),
      decision({ item_id: 'a', subject: 'Urgent', tier: 'now', label_id: 'l1' }),
    );
    openDay();
    await screen.findByText('Urgent');
    const headings = screen.getAllByRole('heading', { level: 4 }).map((h) => h.textContent);
    expect(headings).toEqual(['Now1', 'Client1', 'To sort1']);
  });
});

describe("a stranger's text", () => {
  const HOSTILE = '<img src=x onerror="window.pwned=1"> **urgent** [click](http://evil.example)';

  it('stays inert text: no element, no link, no markdown', async () => {
    theDay(
      server,
      decision({ subject: HOSTILE, from_name: '<b>Boss</b>', from: 'x@evil.example' }),
    );
    openDay();
    const subject = await screen.findByText(HOSTILE);
    expect(subject.textContent).toBe(HOSTILE);
    const row = itemAround(subject);
    expect(row.querySelector('img')).toBeNull();
    expect(row.querySelector('a')).toBeNull();
    expect(row.querySelector('b')).toBeNull();
    expect(row.querySelector('strong')).toBeNull();
    expect(within(row).getByText('<b>Boss</b>')).toBeTruthy();
    expect((window as unknown as { pwned?: number }).pwned).toBeUndefined();
  });

  it('isolates each piece, so an override cannot run into the address', async () => {
    theDay(server, decision({ subject: '‮gnp.exe', from_name: 'Name' }));
    openDay();
    const subject = await screen.findByText('‮gnp.exe');
    expect(subject.tagName).toBe('BDI');
    expect(screen.getByText('Name').tagName).toBe('BDI');
    expect(screen.getByText('ada@example.com').tagName).toBe('BDI');
  });

  it('never truncates an address, because its domain comes last', async () => {
    theDay(server, decision({ from: 'a.very.long.local.part.of.an.address@evil.example' }));
    openDay();
    const address = await screen.findByText('a.very.long.local.part.of.an.address@evil.example');
    expect(address.className).not.toContain('truncate');
    expect(address.className).toContain('break-all');
  });

  it('shows the address alone when there is no name', async () => {
    theDay(server, decision({ from_name: '', from: 'organizer@x.org' }));
    openDay();
    expect(await screen.findByText('organizer@x.org')).toBeTruthy();
  });

  it('names what it cannot read', async () => {
    theDay(server, decision({ from: '', from_name: '', subject: '' }));
    openDay();
    expect(await screen.findByText('Unknown sender')).toBeTruthy();
    expect(screen.getByText('No subject')).toBeTruthy();
  });

  it('says what happened to an event before its subject', async () => {
    theDay(server, decision({ resource: 'calendar', subject: 'Audit', change: 'moved' }));
    openDay();
    expect(await screen.findByText('Moved:')).toBeTruthy();
    expect(screen.getByText('Audit')).toBeTruthy();
  });

  it('reads as the word alone when the event names nothing', async () => {
    theDay(
      server,
      decision({
        resource: 'calendar',
        from: '',
        from_name: '',
        subject: '',
        start: '2026-11-02T12:00:00Z',
        change: 'cancelled',
      }),
    );
    openDay();
    expect(await screen.findByText('Cancelled')).toBeTruthy();
    expect(screen.queryByText('No subject')).toBeNull();
    expect(screen.queryByText('Unknown sender')).toBeNull();
  });
});

describe('why', () => {
  it.each([
    ['rule', 'One of your rules decided this'],
    ['people', 'You have written to this person before'],
    ['person', 'You decided this'],
  ])('%s reads as a sentence', async (decidedBy, sentence) => {
    theDay(server, decision({ decided_by: decidedBy }));
    openDay();
    expect(await screen.findByText(sentence)).toBeTruthy();
  });

  it('explains a verdict the model decided', async () => {
    theDay(server, decision({ decided_by: 'model' }));
    openDay();
    expect(await screen.findByText('The model decided')).toBeTruthy();
    cleanup();
    await i18n.changeLanguage('it');
    openDay();
    expect(await screen.findByText("L'ha deciso il modello")).toBeTruthy();
  });

  it('marks a sender that failed authentication', async () => {
    theDay(server, decision({ auth_failed: true, decided_by: 'auth_failed' }));
    openDay();
    expect(await screen.findByText('Unverified sender')).toBeTruthy();
  });

  it('marks it even when a rule decided, because the mark outlives the rung', async () => {
    theDay(server, decision({ auth_failed: true, decided_by: 'rule' }));
    openDay();
    expect(await screen.findByText('Unverified sender')).toBeTruthy();
  });

  it('shows the tier the centroid would have given where it disagrees with the rules', async () => {
    theDay(server, decision({ tier: 'today', centroid_tier: 'now' }));
    openDay();
    expect(await screen.findByText('From similar mail it would have said Now')).toBeTruthy();
  });

  it('says it in the identity’s language', async () => {
    await i18n.changeLanguage('it');
    theDay(server, decision({ tier: 'today', centroid_tier: 'later' }));
    openDay();
    expect(await screen.findByText('Da mail simili avrebbe detto Dopo')).toBeTruthy();
  });

  it('marks only the label the model gave and nobody changed', async () => {
    theDay(
      server,
      decision({ item_id: 'a', subject: 'Offer', label_id: 'l1', label: 'Ads', model_label: true }),
      decision({ item_id: 'b', subject: 'Report', label_id: 'l2', label: 'Work' }),
    );
    openDay();
    await screen.findByText('Offer');
    expect(within(rowOf('Offer')).getByText('by the model')).toBeTruthy();
    expect(within(rowOf('Report')).queryByText('by the model')).toBeNull();
  });

  it('marks it in the identity’s language', async () => {
    await i18n.changeLanguage('it');
    theDay(server, decision({ label_id: 'l1', label: 'Pubblicità', model_label: true }));
    openDay();
    expect(await screen.findByText('dal modello')).toBeTruthy();
  });

  it('says nothing of the centroid where it agrees', async () => {
    theDay(server, decision({ decided_by: 'people' }));
    openDay();
    expect(await screen.findByText('You have written to this person before')).toBeTruthy();
    expect(screen.queryByText(/similar mail/)).toBeNull();
  });
});

describe('a gone item', () => {
  it('reads as gone, with its tier and no control', async () => {
    theDay(server, decision({ gone: true, from: '', from_name: '', subject: '', tier: 'now' }));
    openDay();
    const row = itemAround(await screen.findByText('No longer exists'));
    expect(within(row).queryAllByRole('radio')).toEqual([]);
    expect(within(row).queryByRole('combobox')).toBeNull();
    expect(within(row).getByText('Now')).toBeTruthy();
  });

  it('takes no tier and no label from a key', async () => {
    server.on('GET /api/mail-watch/labels', () => ({ body: [label()] }));
    theDay(server, decision({ gone: true, from: '', from_name: '', subject: '' }));
    openDay();
    await screen.findByText('No longer exists');
    press('1');
    press('l');
    await settled();
    expect(server.writes('POST', CORRECT)).toEqual([]);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('names an event that is gone', async () => {
    theDay(
      server,
      decision({ gone: true, resource: 'calendar', from: '', from_name: '', subject: '' }),
    );
    openDay();
    expect(await screen.findByRole('button', { name: /Calendar event/ })).toBeTruthy();
  });
});

describe('an event', () => {
  it('shows the date of an all-day event and no time', async () => {
    theDay(
      server,
      decision({
        resource: 'calendar',
        decided_by: 'calendar',
        start: '2026-11-03T00:00:00Z',
        all_day: true,
      }),
    );
    openDay();
    const time = itemAround(await screen.findByText('Quarterly numbers')).querySelector('time');
    expect(time?.textContent).toMatch(/Nov 3|3 Nov/);
    expect(time?.textContent).not.toMatch(/:/);
  });

  it('corrects as a calendar item, and offers no rule', async () => {
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'now' } }));
    theDay(server, decision({ resource: 'calendar', item_id: 'e1', subject: 'Review' }));
    openDay();
    await screen.findByText('Review');
    expect(screen.queryByRole('button', { name: /Always like this/ })).toBeNull();
    press('1');
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)[0]?.body).toEqual({
        resource: 'calendar',
        item_id: 'e1',
        tier: 'now',
      });
    });
  });
});
describe('opening a row', () => {
  const LONG = `${'A very long subject that a phone clamps '.repeat(6)}and then ends here`;

  it('shows the whole subject and when it was decided, then closes again', async () => {
    theDay(server, decision({ subject: LONG, decided_at: '2026-10-28T09:12:00Z' }));
    openDay();
    const subject = await screen.findByText(LONG);
    const clamp = () => subject.parentElement?.className ?? '';
    const button = rowOf('A very long subject');
    expect(clamp()).toContain('line-clamp-2');
    expect(screen.queryByText(/Decided on/)).toBeNull();

    fireEvent.click(button);
    expect(button.getAttribute('aria-expanded')).toBe('true');
    expect(clamp()).not.toContain('line-clamp-2');
    expect(screen.getByText(/Decided on/)).toBeTruthy();

    fireEvent.click(button);
    expect(button.getAttribute('aria-expanded')).toBe('false');
    expect(clamp()).toContain('line-clamp-2');
  });

  it('is a real button, which is what makes Enter and Space open it', async () => {
    theDay(server, decision());
    openDay();
    await screen.findByText('Quarterly numbers');
    expect(activeRow().tagName).toBe('BUTTON');
    expect(activeRow().getAttribute('type')).toBe('button');
  });
});
describe('in Italian', () => {
  it('reads the fixed words of the day', async () => {
    await i18n.changeLanguage('it');
    server.on('GET /api/mail-watch/labels', () => ({ body: [label()] }));
    theDay(server, decision({ decided_by: 'people' }));
    openDay();
    expect(await screen.findByText('Hai già scritto a questa persona')).toBeTruthy();
    expect(screen.getByRole('heading', { name: /Da smistare/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /D'ora in poi sempre così/ })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Giornata' })).toBeTruthy();
  });

  it('says a learned rule the way the brief asks', async () => {
    await i18n.changeLanguage('it');
    server.on(`POST ${CORRECT}`, () => ({
      body: { tier: 'later', rule: rule(), rule_created: true },
    }));
    theDay(server, decision());
    openDay();
    await screen.findByText('Quarterly numbers');
    press('3');
    expect(await screen.findByText('Seconda correzione: ora è una regola')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Annulla' })).toBeTruthy();
  });
});
