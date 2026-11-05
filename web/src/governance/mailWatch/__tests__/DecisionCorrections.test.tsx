import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import '../../../i18n/i18n';
import {
  CORRECT,
  DECISIONS,
  dateInput,
  itemOf,
  openDay,
  press,
  rowOf,
  theDay,
  THREE,
  tierOf,
} from '../../../test/mailWatchDay';
import {
  decision,
  hang,
  label,
  labelRule,
  mailWatchServer,
  rule,
  settled,
  watchDay,
} from '../../../test/mailWatchServer';

// What a correction changes, what it teaches, and the one-click rule a row offers.

let server: ReturnType<typeof mailWatchServer>;

beforeEach(() => {
  server = mailWatchServer();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe('correcting one mail', () => {
  it('moves the row at once, before the server answers, and needs no rule', async () => {
    server.on(`POST ${CORRECT}`, hang);
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    press('1');
    await waitFor(() => {
      expect(tierOf('One')).toBe('Now');
    });
    expect(screen.getByText('You decided this')).toBeTruthy();
    expect(server.writes('POST', '/api/mail-watch/rules')).toEqual([]);
  });

  it('marks the row busy and takes no second correction on it', async () => {
    server.on(`POST ${CORRECT}`, hang);
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    press('1');
    await waitFor(() => {
      expect(rowOf('One').getAttribute('aria-busy')).toBe('true');
    });
    press('3');
    await settled();
    expect(server.writes('POST', CORRECT)).toHaveLength(1);
  });

  it('puts the row back and says so when the server refuses', async () => {
    server.on(`POST ${CORRECT}`, () => ({ status: 502, body: { error: 'provider_auth' } }));
    theDay(server, decision({ subject: 'One', decided_by: 'people' }));
    openDay();
    await screen.findByText('One');
    press('1');
    expect((await screen.findByRole('alert')).textContent).toMatch(/back as it was/);
    expect(tierOf('One')).toBe('Today');
    expect(screen.getByText('You have written to this person before')).toBeTruthy();
  });

  it('changes a label on its own, leaving the tier out', async () => {
    server.on('GET /api/mail-watch/labels', () => ({
      body: [label({ id: 'l1', name: 'Client' })],
    }));
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'today', label_id: 'l1' } }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    fireEvent.click(within(itemOf('One')).getByRole('combobox', { name: 'Label' }));
    fireEvent.click(await screen.findByRole('option', { name: 'Client' }));
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)[0]?.body).toEqual({
        resource: 'mail',
        item_id: 'm1',
        label_id: 'l1',
      });
    });
    expect(await screen.findByRole('heading', { name: /Client/ })).toBeTruthy();
  });

  it('clears a label with an empty id, and the row goes back to To sort', async () => {
    server.on('GET /api/mail-watch/labels', () => ({
      body: [label({ id: 'l1', name: 'Client' })],
    }));
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'today' } }));
    theDay(server, decision({ subject: 'One', label_id: 'l1' }));
    openDay();
    await screen.findByText('One');
    fireEvent.click(within(itemOf('One')).getByRole('combobox', { name: 'Label' }));
    fireEvent.click(await screen.findByRole('option', { name: 'To sort' }));
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)[0]?.body).toMatchObject({ label_id: '' });
    });
    expect(await screen.findByRole('heading', { name: /To sort/ })).toBeTruthy();
  });

  it('moves the row to the discarded list when the label discards', async () => {
    let moved = false;
    server.on('GET /api/mail-watch/labels', () => ({
      body: [label({ id: 'spam', name: 'Spam', discard: true })],
    }));
    server.on(`POST ${CORRECT}`, () => {
      moved = true;
      return { body: { tier: 'later', label_id: 'spam', discarded: true } };
    });
    server.on(DECISIONS, (call) => {
      const mine = decision({ subject: 'One', tier: 'later', label_id: 'spam', discarded: true });
      if (call.query.includes('discarded=1'))
        return { body: watchDay(moved ? [mine] : [], undefined, 1) };
      return {
        body: watchDay(moved ? [] : [decision({ subject: 'One' })], undefined, moved ? 1 : 0),
      };
    });
    openDay();
    await screen.findByText('One');
    fireEvent.click(within(itemOf('One')).getByRole('combobox', { name: 'Label' }));
    fireEvent.click(await screen.findByRole('option', { name: 'Spam' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Discarded (1)' }));
    expect(await screen.findByText('One')).toBeTruthy();
    expect(screen.getByText('Discarded')).toBeTruthy();
    expect(tierOf('One')).toBe('Later');
  });

  it('reads the day again after a later correction cancelled the read that moves a row', async () => {
    let discarded = false;
    let keptReads = 0;
    server.on('GET /api/mail-watch/labels', () => ({
      body: [label({ id: 'spam', name: 'Spam', discard: true })],
    }));
    server.on(`POST ${CORRECT}`, (call) => {
      const { item_id: item } = call.body as { item_id: string };
      if (item === 'a') discarded = true;
      return {
        body: item === 'a' ? { tier: 'later', label_id: 'spam', discarded: true } : { tier: 'now' },
      };
    });
    server.on(DECISIONS, (call) => {
      if (call.query.includes('discarded=1')) {
        const mine = decision({ item_id: 'a', subject: 'Alpha', tier: 'later', discarded: true });
        return { body: watchDay(discarded ? [mine] : [], undefined, discarded ? 1 : 0) };
      }
      keptReads += 1;
      // The read that follows the move hangs, as a slow PIM does, until the next correction cancels it.
      if (discarded && keptReads === 2) return hang();
      const alpha = decision({ item_id: 'a', subject: 'Alpha', from: 'a@x.org' });
      const bravo = decision({ item_id: 'b', subject: 'Bravo', from: 'b@x.org' });
      return { body: watchDay(discarded ? [bravo] : [alpha, bravo], undefined, discarded ? 1 : 0) };
    });
    openDay();
    await screen.findByText('Alpha');
    fireEvent.click(within(itemOf('Alpha')).getByRole('combobox', { name: 'Label' }));
    fireEvent.click(await screen.findByRole('option', { name: 'Spam' }));
    await waitFor(() => {
      expect(keptReads).toBe(2);
    });
    press('1', rowOf('Bravo'));
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)).toHaveLength(2);
    });
    await waitFor(() => {
      expect(screen.queryByText('Alpha')).toBeNull();
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Discarded (1)' }));
    expect(await screen.findByText('Alpha')).toBeTruthy();
  });

  it('corrects with the tier control too, and pressing the current tier does nothing', async () => {
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'later' } }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    fireEvent.click(within(itemOf('One')).getByRole('radio', { name: 'Today' }));
    await settled();
    expect(server.writes('POST', CORRECT)).toEqual([]);
    fireEvent.click(within(itemOf('One')).getByRole('radio', { name: 'Later' }));
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)).toHaveLength(1);
    });
  });
});

describe('a rule learned is said out loud', () => {
  const LEARNED = rule({
    id: 'learned',
    key: 'ada@example.com',
    tier: 'later',
    source: 'correction',
  });

  function teaches() {
    server.on(`POST ${CORRECT}`, () => ({
      body: { tier: 'later', rule: LEARNED, rule_created: true },
    }));
  }

  it('says it, and Undo deletes the rule', async () => {
    teaches();
    server.on('DELETE /api/mail-watch/rules/learned', () => ({ body: { removed: true } }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    press('3');
    expect(await screen.findByText('Second correction: it is a rule now')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect(await screen.findByText('Rule undone')).toBeTruthy();
    expect(server.writes('DELETE', '/api/mail-watch/rules/learned')).toHaveLength(1);
    expect(screen.queryByText('Second correction: it is a rule now')).toBeNull();
  });

  it("offers no Undo for a rule it merged into, so the person's words are safe", async () => {
    const before = labelRule('l1', { id: 'learned' });
    server.on('GET /api/mail-watch/rules', () => ({ body: [before] }));
    server.on(`POST ${CORRECT}`, () => ({
      body: { tier: 'later', rule: { ...before, tier: 'later', source: 'correction' } },
    }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    await waitFor(() => {
      expect(server.calls.some((call) => call.path === '/api/mail-watch/rules')).toBe(true);
    });
    press('3');
    expect(await screen.findByText('Second correction: it is a rule now')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull();
  });

  it('offers no Undo for a merge whatever the cockpit has cached', async () => {
    server.on(`POST ${CORRECT}`, () => ({
      body: { tier: 'later', rule: { ...LEARNED, label_id: 'l1' } },
    }));
    server.on('GET /api/mail-watch/rules', hang);
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    press('3');
    expect(await screen.findByText('Second correction: it is a rule now')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull();
  });

  it('says nothing when the correction agrees with the rule on file', async () => {
    teaches();
    server.on('GET /api/mail-watch/rules', () => ({ body: [LEARNED] }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    await waitFor(() => {
      expect(server.calls.some((call) => call.path === '/api/mail-watch/rules')).toBe(true);
    });
    press('3');
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)).toHaveLength(1);
    });
    await waitFor(() => {
      expect(rowOf('One').hasAttribute('aria-busy')).toBe(false);
    });
    expect(screen.queryByText('Second correction: it is a rule now')).toBeNull();
  });

  it('says so when Undo fails, and keeps the button', async () => {
    teaches();
    server.on('DELETE /api/mail-watch/rules/learned', () => ({
      status: 500,
      body: { error: 'mail_watch_failed' },
    }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    press('3');
    fireEvent.click(await screen.findByRole('button', { name: 'Undo' }));
    expect((await screen.findByRole('alert')).textContent).toMatch(/could not be undone/);
    expect(screen.getByRole('button', { name: 'Undo' })).toBeTruthy();
  });

  it('can be dismissed', async () => {
    teaches();
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    press('3');
    fireEvent.click(await screen.findByRole('button', { name: 'Dismiss' }));
    expect(screen.queryByText('Second correction: it is a rule now')).toBeNull();
  });

  it('speaks through a live region that was already there', async () => {
    teaches();
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    const region = screen.getAllByRole('status').at(-1);
    press('3');
    await screen.findByText('Second correction: it is a rule now');
    expect(region?.textContent).toContain('Second correction: it is a rule now');
  });
});

describe('from now on, always like this', () => {
  const ALWAYS = { name: /Always like this/ };

  it('makes a rule from the row in one click, and it can be undone', async () => {
    const saved = rule({ id: 'new', tier: 'today', label_id: 'l1' });
    server.on('POST /api/mail-watch/rules', () => ({ body: { ...saved, rule_created: true } }));
    server.on('DELETE /api/mail-watch/rules/new', () => ({ body: { removed: true } }));
    theDay(server, decision({ subject: 'One', label_id: 'l1' }));
    openDay();
    await screen.findByText('One');
    fireEvent.click(screen.getByRole('button', ALWAYS));
    expect(await screen.findByText('Rule saved')).toBeTruthy();
    expect(server.writes('POST', '/api/mail-watch/rules')[0]?.body).toEqual({
      key: 'ada@example.com',
      tier: 'today',
      label_id: 'l1',
    });
    fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect(await screen.findByText('Rule undone')).toBeTruthy();
  });

  it('offers no Undo for a rule that replaced one, whatever the cockpit has cached', async () => {
    server.on('POST /api/mail-watch/rules', () => ({ body: rule({ id: 'kept', tier: 'today' }) }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    fireEvent.click(screen.getByRole('button', ALWAYS));
    expect(await screen.findByText('Rule saved')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull();
  });

  it('appears on the active row only', async () => {
    theDay(server, ...THREE);
    openDay();
    await screen.findByText('First');
    expect(screen.getAllByRole('button', ALWAYS)).toHaveLength(1);
    expect(within(itemOf('First')).getByRole('button', ALWAYS)).toBeTruthy();
  });

  it('is not offered on a sender that failed authentication', async () => {
    theDay(server, decision({ auth_failed: true }));
    openDay();
    await screen.findByText('Quarterly numbers');
    expect(screen.queryByRole('button', ALWAYS)).toBeNull();
  });

  it('is not offered when that rule is already on file', async () => {
    server.on('GET /api/mail-watch/rules', () => ({
      body: [rule({ key: 'ada@example.com', tier: 'today' })],
    }));
    theDay(server, decision({ from: 'Ada@Example.com' }));
    openDay();
    await screen.findByText('Quarterly numbers');
    await waitFor(() => {
      expect(screen.queryByRole('button', ALWAYS)).toBeNull();
    });
  });

  it('says what it would have changed in the rest of the day', async () => {
    theDay(
      server,
      decision({ item_id: 'a', subject: 'One', tier: 'now' }),
      decision({ item_id: 'b', subject: 'Two' }),
      decision({ item_id: 'c', subject: 'Three', tier: 'later' }),
    );
    openDay();
    await screen.findByText('One');
    expect(screen.getByText('Would have changed 2 more mail today')).toBeTruthy();
  });

  it('says the day it means when it is not today', async () => {
    theDay(server, decision({ item_id: 'a', tier: 'now' }), decision({ item_id: 'b' }));
    openDay();
    await screen.findByText(/Would have changed 1 more mail today/);
    fireEvent.change(dateInput(), { target: { value: '2026-10-28' } });
    expect(await screen.findByText(/Would have changed 1 more mail on October 28/)).toBeTruthy();
  });

  it('stays quiet when nothing else would change', async () => {
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    expect(screen.queryByText(/Would have changed/)).toBeNull();
  });

  it('says what a refused rule means, and leaves the mail alone', async () => {
    server.on('POST /api/mail-watch/rules', () => ({
      status: 400,
      body: { error: 'invalid_rule_key', message: 'a rule names an address or a domain' },
    }));
    theDay(server, decision({ subject: 'One' }));
    openDay();
    await screen.findByText('One');
    fireEvent.click(screen.getByRole('button', ALWAYS));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'Write a whole address, or a domain such as company.com.',
    );
  });
});
