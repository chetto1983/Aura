import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n';
import {
  account,
  hang,
  mailWatchServer,
  renderWatch,
  watchDay,
  watchStatus,
} from '../../../test/mailWatchServer';
import type { PimAccount } from '../../pimApi';
import { AccountWatch } from '../AccountWatch';
import { clockTime } from '../mailWatchLogic';

// The switch on an account's row: what it says for each state, and what flipping it sends.

const PIM: PimAccount = { id: 'work', displayName: 'Work', provider: 'google', enabled: true };
const STATUS = '/api/mail-watch';

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function statusWith(...accounts: readonly ReturnType<typeof account>[]) {
  server.on(`GET ${STATUS}`, () => ({ body: watchStatus({ accounts }) }));
}

async function theSwitch() {
  return screen.findByRole('switch', { name: 'Mail and calendar watch for Work' });
}

describe('an account with no watch', () => {
  beforeEach(() => {
    statusWith();
  });

  it('offers the switch, off, with what it would do', async () => {
    renderWatch(<AccountWatch account={PIM} />);
    expect(await theSwitch()).toHaveProperty('ariaChecked', 'false');
    expect(screen.getByText(/one digest a day/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Open the day' })).toBeNull();
  });

  it.each([
    ['off', []],
    ['on', [account()]],
  ])('says it reads the Inbox only with the switch %s', async (_state, accounts) => {
    statusWith(...accounts);
    renderWatch(<AccountWatch account={PIM} />);
    await theSwitch();
    expect(screen.getByText(/mail a filter moves to other folders is not watched/)).toBeTruthy();
  });

  it('turns on through the enable route', async () => {
    server.on('POST /api/mail-watch/accounts/work/enable', () => {
      statusWith(account());
      return { body: { enabled: true } };
    });
    renderWatch(<AccountWatch account={PIM} />);
    fireEvent.click(await theSwitch());
    expect(await screen.findByText('Watching')).toBeTruthy();
    expect((await theSwitch()).getAttribute('aria-checked')).toBe('true');
  });

  it('shows the flip while it is on its way, and stays busy', async () => {
    server.on('POST /api/mail-watch/accounts/work/enable', hang);
    renderWatch(<AccountWatch account={PIM} />);
    const control = await theSwitch();
    fireEvent.click(control);
    await waitFor(() => {
      expect(control.getAttribute('aria-checked')).toBe('true');
    });
    expect(control.hasAttribute('disabled')).toBe(true);
  });

  it('says what a refused enable means, and goes back to off', async () => {
    server.on('POST /api/mail-watch/accounts/work/enable', () => ({
      status: 502,
      body: { error: 'provider_auth' },
    }));
    renderWatch(<AccountWatch account={PIM} />);
    fireEvent.click(await theSwitch());
    expect((await screen.findByRole('alert')).textContent).toMatch(/no longer sign in/);
    expect((await theSwitch()).getAttribute('aria-checked')).toBe('false');
  });
});

describe('a running watch', () => {
  it('says it is watching, with the alerts against the cap and the next run', async () => {
    const nextRun = '2026-11-01T14:35:00Z';
    statusWith(account({ alerts_today: 2, next_run_at: nextRun }));
    renderWatch(<AccountWatch account={PIM} />);
    expect(await screen.findByText('Watching')).toBeTruthy();
    const line = screen.getByText(/2 of 5 alerts today/);
    expect(line.textContent).toContain(`next check ${clockTime(nextRun, 'en')}`);
    expect(line.textContent).not.toMatch(/daily cap reached/);
  });

  it('says when the daily cap is reached', async () => {
    statusWith(account({ alerts_today: 5 }));
    renderWatch(<AccountWatch account={PIM} />);
    expect(await screen.findByText(/daily cap reached/)).toBeTruthy();
  });

  it('holds no count against a cap of 0, which means no alerts', async () => {
    server.on(`GET ${STATUS}`, () => ({
      body: watchStatus({
        settings: { ...watchStatus().settings, daily_alert_cap: 0 },
        accounts: [account({ alerts_today: 0, next_run_at: null })],
      }),
    }));
    renderWatch(<AccountWatch account={PIM} />);
    expect(await screen.findByText('Watching')).toBeTruthy();
    expect(screen.queryByText(/alerts today/)).toBeNull();
    expect(screen.queryByText(/daily cap reached/)).toBeNull();
  });

  it('turns off through the disable route', async () => {
    statusWith(account());
    server.on('POST /api/mail-watch/accounts/work/disable', () => {
      statusWith();
      return { body: { enabled: false } };
    });
    renderWatch(<AccountWatch account={PIM} />);
    fireEvent.click(await theSwitch());
    expect(await screen.findByText(/one digest a day/)).toBeTruthy();
  });

  it('opens the day in a dialog named for the account', async () => {
    statusWith(account());
    server.on('GET /api/mail-watch/accounts/work/decisions', () => ({ body: watchDay([]) }));
    renderWatch(<AccountWatch account={PIM} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Open the day' }));
    expect(await screen.findByRole('dialog', { name: 'Work' })).toBeTruthy();
  });

  it('falls back on the id for an account with no name', async () => {
    statusWith(account());
    renderWatch(<AccountWatch account={{ ...PIM, displayName: '' }} />);
    expect(await screen.findByRole('switch', { name: /watch for work/ })).toBeTruthy();
  });
});

describe('a paused watch', () => {
  it.each([
    ['operator', 'Paused by hand'],
    ['failures', 'Paused after repeated failures'],
  ])('%s says why and offers to resume', async (reason, sentence) => {
    statusWith(account({ paused: true, paused_reason: reason, next_run_at: null }));
    server.on('POST /api/mail-watch/accounts/work/enable', () => {
      statusWith(account());
      return { body: { enabled: true } };
    });
    renderWatch(<AccountWatch account={PIM} />);
    expect(await screen.findByText(sentence)).toBeTruthy();
    expect(screen.queryByText('Watching')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }));
    expect(await screen.findByText('Watching')).toBeTruthy();
    expect(server.writes('POST', '/api/mail-watch/accounts/work/enable')).toHaveLength(1);
  });

  it('has nothing to resume after a deactivation', async () => {
    statusWith(account({ paused: true, paused_reason: 'identity_deactivated' }));
    renderWatch(<AccountWatch account={PIM} />);
    expect(await screen.findByText('Paused because this identity was deactivated')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Resume' })).toBeNull();
  });

  it('stays switched on, so turning it off is still one flip', async () => {
    statusWith(account({ paused: true, paused_reason: 'failures' }));
    renderWatch(<AccountWatch account={PIM} />);
    expect((await theSwitch()).getAttribute('aria-checked')).toBe('true');
  });
});

describe('a resource', () => {
  const failing = {
    resource: 'mail',
    status: 'failing',
    failing_since: null,
    advanced_at: null,
  } as const;

  it('says what failed in words, with the code translated', async () => {
    statusWith(account({ resources: [{ ...failing, last_error: 'provider_auth' }] }));
    renderWatch(<AccountWatch account={PIM} />);
    const line = await screen.findByText(/no longer sign in/);
    expect(line.textContent).toMatch(/^Mail:/);
  });

  it('says since when', async () => {
    const since = new Date(Date.now() - 60_000).toISOString();
    const resources = [{ ...failing, last_error: 'pim_http_500', failing_since: since }];
    statusWith(account({ resources }));
    renderWatch(<AccountWatch account={PIM} />);
    const line = await screen.findByText(/status 500/);
    expect(line.textContent).toContain(`Mail, since ${clockTime(since, 'en')}:`);
  });

  it('survives a failing resource with no code', async () => {
    statusWith(account({ resources: [failing] }));
    renderWatch(<AccountWatch account={PIM} />);
    expect(await screen.findByText(/reported a problem/)).toBeTruthy();
  });

  it('says calmly what an account does not offer', async () => {
    const resources = [
      { resource: 'calendar', status: 'unsupported', failing_since: null, advanced_at: null },
    ] as const;
    statusWith(account({ resources }));
    renderWatch(<AccountWatch account={PIM} />);
    expect(await screen.findByText('Calendar: not offered by this account')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('describes the switch with its state', async () => {
    statusWith(account());
    renderWatch(<AccountWatch account={PIM} />);
    const control = await theSwitch();
    const stateId = control.getAttribute('aria-describedby') ?? '';
    expect(document.getElementById(stateId)?.textContent).toContain('Watching');
  });
});

describe('a deployment that cannot watch', () => {
  it('draws nothing on a row with no watch', async () => {
    server.on(`GET ${STATUS}`, () => ({
      body: watchStatus({ configured: false, reason: 'no sidecar', accounts: [] }),
    }));
    const { container } = renderWatch(<AccountWatch account={PIM} />);
    await waitFor(() => {
      expect(server.calls.length).toBeGreaterThan(0);
    });
    await waitFor(() => {
      expect(container.textContent).toBe('');
    });
  });

  it('keeps the switch of an existing watch, because turning off still works', async () => {
    server.on(`GET ${STATUS}`, () => ({
      body: watchStatus({ configured: false, reason: 'no sidecar' }),
    }));
    renderWatch(<AccountWatch account={PIM} />);
    expect((await theSwitch()).getAttribute('aria-checked')).toBe('true');
  });

  it('draws nothing while the status is unknown', () => {
    server.on(`GET ${STATUS}`, hang);
    const { container } = renderWatch(<AccountWatch account={PIM} />);
    expect(container.textContent).toBe('');
  });
});
