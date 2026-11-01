import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import '../../../i18n/i18n';
import {
  CORRECT,
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
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'later', rule: LEARNED } }));
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

  it("puts back a rule it merged into, so Undo never loses the person's words", async () => {
    const before = labelRule('l1', { id: 'learned' });
    server.on('GET /api/mail-watch/rules', () => ({ body: [before] }));
    server.on('POST /api/mail-watch/rules', () => ({ body: before }));
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
    fireEvent.click(await screen.findByRole('button', { name: 'Undo' }));
    expect(await screen.findByText('Rule undone')).toBeTruthy();
    expect(server.writes('DELETE', '/api/mail-watch/rules/learned')).toEqual([]);
    expect(server.writes('POST', '/api/mail-watch/rules')[0]?.body).toEqual({
      key: 'ada@example.com',
      label_id: 'l1',
    });
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
    server.on('POST /api/mail-watch/rules', () => ({ body: saved }));
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
