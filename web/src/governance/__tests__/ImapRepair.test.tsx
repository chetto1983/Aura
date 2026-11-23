import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, screen } from '@testing-library/react';
import type { QueryClient } from '@tanstack/react-query';
import '../../i18n/i18n';
import {
  account,
  mailWatchServer,
  renderWatch,
  settled,
  watchStatus,
} from '../../test/mailWatchServer';
import { MAIL_WATCH_STATUS_KEY } from '../mailWatch/useMailWatch';
import { PIM_ACCOUNTS_KEY, type PimAccount } from '../pimApi';
import { PimAccountRow } from '../PimAccountRow';

// Repair on an IMAP account's row: «Prova di nuovo» checks the stored login again, «Cambia password»
// sends a new password (and any server field typed in) on the same account. Both run through the
// real pimApi at the fetch boundary, so a 422 is read the way the browser reads it.

const BOX: PimAccount = {
  id: 'box',
  displayName: 'mario@example.it',
  provider: 'imap',
  enabled: true,
  linked: null,
};
const VERIFY = '/api/connect/pim/accounts/box/verify';
const LOGIN_WORKS =
  'Sign-in works. If reading does not resume at the next check, the password is not the problem.';
const PATCH = '/api/connect/pim/accounts/box/provider-config';

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
  server.on('GET /api/mail-watch', () => ({ body: watchStatus({ accounts: [] }) }));
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function renderRow(acct: PimAccount = BOX) {
  return renderWatch(<PimAccountRow account={acct} removing={false} onRemove={() => undefined} />);
}

async function tryAgain() {
  fireEvent.click(await screen.findByRole('button', { name: 'Try again' }));
}

async function openChangePassword() {
  fireEvent.click(await screen.findByRole('button', { name: 'Change password' }));
}

function typeInto(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

describe('«Prova di nuovo»', () => {
  it('RetryShowsSuccess, and refreshes the accounts and the watch', async () => {
    server.on(`POST ${VERIFY}`, () => ({ body: { ok: true } }));
    const { client } = renderRow();
    const invalidate = vi.spyOn(client, 'invalidateQueries');
    await tryAgain();
    expect(await screen.findByText(LOGIN_WORKS)).toBeTruthy();
    expect(server.writes('POST', VERIFY)).toHaveLength(1);
    const invalidated = invalidate.mock.calls.map(([filters]) => filters);
    expect(invalidated).toContainEqual({ queryKey: PIM_ACCOUNTS_KEY });
    expect(invalidated).toContainEqual({ queryKey: MAIL_WATCH_STATUS_KEY });
  });

  it.each([
    [
      'a refused password',
      422,
      { error: 'verify_failed', step: 'imap', reason: 'auth_failed' },
      'The password was refused: it is wrong, or the provider wants an app password instead of the one you sign in with.',
    ],
    [
      'a server that does not answer',
      422,
      { error: 'verify_failed', step: 'imap', reason: 'unreachable' },
      'The mail server did not answer. Check the server name and port.',
    ],
    [
      'too many attempts',
      429,
      { error: 'too_many_attempts' },
      'Too many attempts. Wait a minute and try again.',
    ],
  ])('RetryShowsTheReason: %s', async (_case, status, body, sentence) => {
    server.on(`POST ${VERIFY}`, () => ({ status, body }));
    renderRow();
    await tryAgain();
    expect((await screen.findByRole('alert')).textContent).toBe(sentence);
    expect(screen.queryByText(LOGIN_WORKS)).toBeNull();
  });

  it('RetryShowsTheSmtpWarning', async () => {
    server.on(`POST ${VERIFY}`, () => ({ body: { ok: true, smtpWarning: 'smtp_unreachable' } }));
    renderRow();
    await tryAgain();
    expect(await screen.findByText(LOGIN_WORKS)).toBeTruthy();
    expect(
      screen.getByText(
        'Reading works, but the sending server did not answer: replies cannot be sent yet.',
      ),
    ).toBeTruthy();
  });
});

describe('«Cambia password»', () => {
  const saved = { ...BOX };

  it('ChangePasswordSendsOnlyThePassword, verbatim', async () => {
    server.on(`PATCH ${PATCH}`, () => ({ body: saved }));
    renderRow();
    await openChangePassword();
    typeInto('New password', '  s3cret word ');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(LOGIN_WORKS)).toBeTruthy();
    expect(server.writes('PATCH', PATCH).map((call) => call.body)).toEqual([
      { providerConfig: { password: '  s3cret word ' } },
    ]);
    expect(screen.queryByLabelText('New password')).toBeNull();
  });

  it('ChangingTheServerAsksForThePassword before anything is sent', async () => {
    server.on(`PATCH ${PATCH}`, () => ({ body: saved }));
    renderRow();
    await openChangePassword();
    typeInto('IMAP host', ' imap.new.example ');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('A change of server needs the password again.')).toBeTruthy();
    await settled();
    expect(server.writes('PATCH', PATCH)).toHaveLength(0);

    typeInto('New password', 'pw');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(LOGIN_WORKS)).toBeTruthy();
    expect(server.writes('PATCH', PATCH)[0]?.body).toEqual({
      providerConfig: { imapHost: 'imap.new.example', password: 'pw' },
    });
  });

  it('says why a save was refused, on the same form', async () => {
    server.on(`PATCH ${PATCH}`, () => ({ status: 409, body: { error: 'changed' } }));
    renderRow();
    await openChangePassword();
    typeInto('New password', 'pw');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'The account changed meanwhile. Reload and try again.',
    );
    expect(screen.getByLabelText('New password')).toBeTruthy();
  });

  it('TheUsernameCannotChange: it says so, and offers no username to edit or send', async () => {
    server.on(`PATCH ${PATCH}`, () => ({ body: saved }));
    renderRow();
    await openChangePassword();
    expect(
      screen.getByText('The username cannot be changed: for another mailbox, add a new account.'),
    ).toBeTruthy();
    expect(screen.queryByLabelText(/username/i)).toBeNull();
    typeInto('New password', 'pw');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText(LOGIN_WORKS);
    expect(server.writes('PATCH', PATCH).map((call) => call.body)).toEqual([
      { providerConfig: { password: 'pw' } },
    ]);
  });

  it('closes on «Annulla» without sending', async () => {
    renderRow();
    await openChangePassword();
    typeInto('New password', 'pw');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByLabelText('New password')).toBeNull();
    await settled();
    expect(server.writes('PATCH', PATCH)).toHaveLength(0);
  });
});

describe('an account that is not IMAP', () => {
  it('OAuthRowHasNoRepair', async () => {
    renderRow({ ...BOX, provider: 'google', linked: true });
    expect(await screen.findByRole('button', { name: 'Sign in again' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Change password' })).toBeNull();
  });
});

describe('after a working login', () => {
  const hoursAgo = (hours: number) => new Date(Date.now() - hours * 3_600_000).toISOString();
  const STALE = /Aura has not been able to read this mailbox/;

  function failingSince(since: string) {
    server.on('GET /api/mail-watch', () => ({
      body: watchStatus({
        accounts: [
          account({
            account_id: 'box',
            resources: [
              {
                resource: 'mail',
                status: 'failing',
                last_error: 'provider_transient',
                failing_since: since,
                advanced_at: null,
              },
            ],
          }),
        ],
      }),
    }));
  }

  /** A status poll after the one the login triggered. */
  async function laterStatus(client: QueryClient) {
    await act(async () => {
      await client.invalidateQueries({ queryKey: MAIL_WATCH_STATUS_KEY });
    });
  }

  it('says what a failure at the next check would mean, keeps its buttons, and takes back the stale prompt', async () => {
    failingSince(hoursAgo(7));
    server.on(`POST ${VERIFY}`, () => ({ body: { ok: true } }));
    const { client } = renderRow();
    await screen.findByText(STALE);
    await tryAgain();
    expect(await screen.findByText(LOGIN_WORKS)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Change password' })).toBeTruthy();
    expect(screen.queryByText(STALE)).toBeNull();
    expect(screen.getByText(/The provider is having trouble/)).toBeTruthy();

    await laterStatus(client);
    expect(screen.queryByText(STALE)).toBeNull();
    expect(screen.getByText(/The provider is having trouble/)).toBeTruthy();
  });

  it('a new failure after it asks for repair again once older than 6 h', async () => {
    failingSince(hoursAgo(7));
    server.on(`POST ${VERIFY}`, () => ({ body: { ok: true } }));
    const { client } = renderRow();
    await screen.findByText(STALE);
    await tryAgain();
    await screen.findByText(LOGIN_WORKS);
    expect(screen.queryByText(STALE)).toBeNull();

    failingSince(hoursAgo(6.5));
    await laterStatus(client);
    expect(await screen.findByText(STALE)).toBeTruthy();
  });
});
