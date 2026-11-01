import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import '../../../i18n/i18n';
import { chooseOption } from '../../../test/chooseOption';
import {
  account,
  decision,
  hang,
  label,
  labelRule,
  mailWatchServer,
  renderWatch,
  rowHolding,
  rule,
  settled,
  watchDay,
  watchStatus,
} from '../../../test/mailWatchServer';
import { RulesPanel } from '../RulesPanel';

// The rules tab: a list, a form that previews before it saves, and a confirmed removal.

const RULES = '/api/mail-watch/rules';

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function keyField() {
  return screen.getByLabelText('Address or domain');
}

function type(value: string) {
  fireEvent.change(keyField(), { target: { value } });
}

function submit() {
  fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
}

describe('the list', () => {
  it('shows each rule with what it does and where it came from', async () => {
    server.on('GET /api/mail-watch/labels', () => ({
      body: [label({ id: 'l1', name: 'Client' })],
    }));
    server.on(`GET ${RULES}`, () => ({
      body: [
        rule({ id: 'a', key: 'ada@example.com', tier: 'now', source: 'person' }),
        labelRule('l1', {
          id: 'b',
          key: 'shop.example',
          key_kind: 'domain',
          source: 'correction',
        }),
      ],
    }));
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText('ada@example.com');
    const sender = rowHolding('ada@example.com');
    expect(sender.getByText('Sender · Written by you')).toBeTruthy();
    expect(sender.getByText('Now')).toBeTruthy();
    const domain = rowHolding('shop.example');
    expect(domain.getByText('Domain · Learned from your corrections')).toBeTruthy();
    expect(domain.getByText('Client')).toBeTruthy();
    expect(domain.queryByText('Now')).toBeNull();
  });

  it('shows a label it cannot name as nothing', async () => {
    server.on(`GET ${RULES}`, () => ({ body: [labelRule('gone')] }));
    renderWatch(<RulesPanel day={null} />);
    const item = await screen.findByRole('listitem');
    expect(within(item).queryByText('gone')).toBeNull();
  });

  it('says so when there are none', async () => {
    renderWatch(<RulesPanel day={null} />);
    expect(await screen.findByText(/No rules yet/)).toBeTruthy();
  });

  it('announces the wait', () => {
    server.on(`GET ${RULES}`, hang);
    renderWatch(<RulesPanel day={null} />);
    expect(screen.getByText('Loading…')).toBeTruthy();
  });

  it('says when it cannot read them', async () => {
    server.on(`GET ${RULES}`, () => ({ status: 500, body: { error: 'mail_watch_failed' } }));
    renderWatch(<RulesPanel day={null} />);
    expect((await screen.findByRole('alert')).textContent).toMatch(/cannot be read/);
  });
});

describe('adding a rule', () => {
  it('sends what the person chose, and clears the form', async () => {
    server.on(`POST ${RULES}`, () => ({ body: rule({ key: 'shop.example', key_kind: 'domain' }) }));
    server.on('GET /api/mail-watch/labels', () => ({
      body: [label({ id: 'l1', name: 'Client' })],
    }));
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('  shop.example ');
    await chooseOption('Tier', 'Later');
    await chooseOption('Label', 'Client');
    submit();
    await waitFor(() => {
      expect(server.writes('POST', RULES)[0]?.body).toEqual({
        key: 'shop.example',
        tier: 'later',
        label_id: 'l1',
      });
    });
    await waitFor(() => {
      expect((keyField() as HTMLInputElement).value).toBe('');
    });
  });

  it('leaves out what the person left unset', async () => {
    server.on(`POST ${RULES}`, () => ({ body: rule() }));
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('ada@example.com');
    await chooseOption('Tier', 'Now');
    submit();
    await waitFor(() => {
      expect(server.writes('POST', RULES)[0]?.body).toEqual({
        key: 'ada@example.com',
        tier: 'now',
      });
    });
  });

  it('marks a bad key only after an attempt, and omits aria-invalid when valid', async () => {
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('not a key');
    expect(keyField().hasAttribute('aria-invalid')).toBe(false);
    submit();
    expect(keyField().getAttribute('aria-invalid')).toBe('true');
    const error = screen.getByText(/whole address, or a domain/);
    expect(keyField().getAttribute('aria-describedby')).toBe(error.id);
    await settled();
    expect(server.writes('POST', RULES)).toEqual([]);
    type('shop.example');
    expect(keyField().hasAttribute('aria-invalid')).toBe(false);
    expect(keyField().hasAttribute('aria-describedby')).toBe(false);
  });

  it('asks for an effect when the key is fine and there is none', async () => {
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('shop.example');
    expect(screen.queryByText('Choose a tier, a label or both.')).toBeNull();
    submit();
    expect(screen.getByText('Choose a tier, a label or both.')).toBeTruthy();
    await settled();
    expect(server.writes('POST', RULES)).toEqual([]);
  });

  it("shows the server's sentence when it refuses", async () => {
    server.on(`POST ${RULES}`, () => ({
      status: 400,
      body: { error: '"x@y" is not an address' },
    }));
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('x@y');
    await chooseOption('Tier', 'Now');
    submit();
    expect((await screen.findByRole('alert')).textContent).toBe('"x@y" is not an address');
  });
});

describe('the preview', () => {
  function dayOfMail() {
    server.on('GET /api/mail-watch/accounts/work/decisions', () => ({
      body: watchDay([
        decision({ item_id: 'a', from: 'one@shop.example' }),
        decision({ item_id: 'b', from: 'two@shop.example' }),
        decision({ item_id: 'c', from: 'three@shop.example', tier: 'later' }),
        decision({ item_id: 'd', from: 'ada@example.com' }),
      ]),
    }));
  }

  it('says what the rule would have changed today, before it is saved', async () => {
    dayOfMail();
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('shop.example');
    await chooseOption('Tier', 'Later');
    expect(await screen.findByText('This rule would have changed 2 mail today.')).toBeTruthy();
    expect(server.writes('POST', RULES)).toEqual([]);
  });

  it('counts across every watched account', async () => {
    dayOfMail();
    server.on('GET /api/mail-watch', () => ({
      body: watchStatus({ accounts: [account(), account({ account_id: 'home' })] }),
    }));
    server.on('GET /api/mail-watch/accounts/home/decisions', () => ({
      body: watchDay([decision({ item_id: 'h', account_id: 'home', from: 'x@shop.example' })]),
    }));
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('shop.example');
    await chooseOption('Tier', 'Later');
    expect(await screen.findByText('This rule would have changed 3 mail today.')).toBeTruthy();
  });

  it('says when it would have changed nothing', async () => {
    dayOfMail();
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('nobody.example');
    await chooseOption('Tier', 'Later');
    expect(
      await screen.findByText('This rule would not have changed any mail today.'),
    ).toBeTruthy();
  });

  it('names the day the dialog is on', async () => {
    dayOfMail();
    renderWatch(<RulesPanel day="2026-10-28" />);
    await screen.findByText(/No rules yet/);
    type('shop.example');
    await chooseOption('Tier', 'Later');
    expect(await screen.findByText(/would have changed 2 mail on October 28\./)).toBeTruthy();
    expect(server.calls.at(-1)?.query).toBe('?day=2026-10-28');
  });

  it('reads nothing from the PIM until the draft is a rule', async () => {
    dayOfMail();
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('shop.example');
    const reads = () => server.calls.filter((call) => call.path.endsWith('/decisions')).length;
    expect(reads()).toBe(0);
    await chooseOption('Tier', 'Later');
    await waitFor(() => {
      expect(reads()).toBe(1);
    });
  });

  it('stays quiet while a day is still out, rather than say zero', async () => {
    server.on('GET /api/mail-watch/accounts/work/decisions', hang);
    renderWatch(<RulesPanel day={null} />);
    await screen.findByText(/No rules yet/);
    type('shop.example');
    await chooseOption('Tier', 'Later');
    expect(screen.queryByText(/would not have changed/)).toBeNull();
    expect(screen.queryByText(/would have changed/)).toBeNull();
  });
});

describe('removing a rule', () => {
  beforeEach(() => {
    server.on(`GET ${RULES}`, () => ({ body: [rule({ id: 'r1' })] }));
  });

  async function askToRemove() {
    fireEvent.click(await screen.findByRole('button', { name: 'Remove rule for ada@example.com' }));
    return screen.findByRole('dialog');
  }

  it('asks first, then removes', async () => {
    server.on(`DELETE ${RULES}/r1`, () => {
      server.on(`GET ${RULES}`, () => ({ body: [] }));
      return { body: { removed: true } };
    });
    renderWatch(<RulesPanel day={null} />);
    const dialog = await askToRemove();
    expect(within(dialog).getByText(/Mail from ada@example.com goes back/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove rule' }));
    expect(await screen.findByText(/No rules yet/)).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('keeps the rule when the person changes their mind', async () => {
    renderWatch(<RulesPanel day={null} />);
    const dialog = await askToRemove();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep it' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(server.writes('DELETE', `${RULES}/r1`)).toEqual([]);
  });

  it('says why a removal failed, inside the dialog', async () => {
    server.on(`DELETE ${RULES}/r1`, () => ({ status: 404, body: { error: 'not_found' } }));
    renderWatch(<RulesPanel day={null} />);
    const dialog = await askToRemove();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove rule' }));
    expect((await within(dialog).findByRole('alert')).textContent).toBe('That is no longer there.');
  });

  it('forgets a failed removal when the dialog closes', async () => {
    server.on(`DELETE ${RULES}/r1`, () => ({ status: 404, body: { error: 'not_found' } }));
    renderWatch(<RulesPanel day={null} />);
    const dialog = await askToRemove();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove rule' }));
    await within(dialog).findByRole('alert');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep it' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    const reopened = await askToRemove();
    expect(within(reopened).queryByRole('alert')).toBeNull();
  });
});
