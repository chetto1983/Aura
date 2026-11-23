import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n';
import { MAIL_WATCH_STATUS_KEY } from '../../../governance/mailWatch/useMailWatch';
import { PIM_ACCOUNTS_KEY, type PimAccount } from '../../../governance/pimApi';
import { mailWatchServer, renderWatch } from '../../../test/mailWatchServer';
import { AddMailAccount } from '../AddMailAccount';

// «Cambia password» on AddMailAccount's "already connected" notice repairs the account the address
// maps to, so it must do what the row's repair (ImapRepair) does: refresh the accounts and the
// watch, and leave no password in the mutation cache once the form is gone. It runs through the
// real pimApi at the fetch boundary.

const ACCOUNTS = '/api/connect/pim/accounts';
const PATCH = `${ACCOUNTS}/mario-libero-it/provider-config`;
const MARIO: PimAccount = {
  id: 'mario-libero-it',
  displayName: 'mario@libero.it',
  provider: 'imap',
  enabled: true,
  linked: null,
};

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
  server.on('POST /api/connect/pim/discover', () => ({
    body: {
      kind: 'imap',
      source: 'preset',
      imapHost: 'imapmail.libero.it',
      imapPort: 993,
      smtpHost: 'smtp.libero.it',
      smtpPort: 465,
    },
  }));
  server.on(`POST ${ACCOUNTS}`, () => ({ status: 409, body: { error: 'duplicate' } }));
  server.on(`GET ${ACCOUNTS}`, () => ({ body: { accounts: [MARIO] } }));
  server.on(`PATCH ${PATCH}`, () => ({ body: MARIO }));
});
afterEach(() => {
  vi.unstubAllGlobals();
});

async function changePasswordOnTheNotice(password: string) {
  const address = screen.getByLabelText('Email address');
  fireEvent.change(address, { target: { value: 'mario@libero.it' } });
  fireEvent.blur(address);
  await waitFor(() => {
    expect(screen.getByLabelText<HTMLInputElement>('IMAP host').value).toBe('imapmail.libero.it');
  });
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: password } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Change password' }));
  await waitFor(() => {
    expect(screen.queryByRole('button', { name: 'Change password' })).toBeNull();
  });
  expect(server.writes('PATCH', PATCH).map((call) => call.body)).toEqual([
    { providerConfig: { password } },
  ]);
}

describe('«Cambia password» on the already-connected notice', () => {
  it('refreshes the accounts and the watch, as the row repair does', async () => {
    const { client } = renderWatch(<AddMailAccount onCreated={() => undefined} />);
    const invalidate = vi.spyOn(client, 'invalidateQueries');
    await changePasswordOnTheNotice('new pass');

    const invalidated = invalidate.mock.calls.map(([filters]) => filters);
    expect(invalidated).toContainEqual({ queryKey: PIM_ACCOUNTS_KEY });
    expect(invalidated).toContainEqual({ queryKey: MAIL_WATCH_STATUS_KEY });
  });

  it('leaves no password in the mutation cache once the form is gone', async () => {
    const { client, unmount } = renderWatch(<AddMailAccount onCreated={() => undefined} />);
    await changePasswordOnTheNotice('s3cret-pw');

    unmount();
    await waitFor(() => {
      const variables = client
        .getMutationCache()
        .getAll()
        .map((mutation) => mutation.state.variables);
      expect(JSON.stringify(variables)).not.toContain('s3cret-pw');
    });
  });
});
