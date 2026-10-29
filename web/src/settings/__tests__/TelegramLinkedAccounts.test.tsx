import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { TelegramLinkedAccounts } from '../TelegramLinkedAccounts';

const PHONE = {
  telegramUserId: 4503599627370495,
  username: 'andrea',
  firstName: 'Andrea',
  addedAt: '2026-10-01T09:00:00Z',
  lastSeenAt: '2026-10-27T18:30:00Z',
};
const TABLET = {
  telegramUserId: 777,
  username: '',
  firstName: '',
  addedAt: '2026-10-02T09:00:00Z',
  lastSeenAt: null,
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

// The list the server holds, and the answer it gives to a DELETE; a 204 removes the row.
function stubServer(accounts: unknown[], unlinkStatus = 204) {
  let rows = [...accounts];
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = urlOf(input);
      const method = init?.method ?? 'GET';
      calls.push(`${method} ${url}`);
      if (method === 'GET' && url === '/api/settings/telegram/accounts') {
        return Promise.resolve(jsonResponse(rows));
      }
      if (method === 'DELETE' && url.startsWith('/api/settings/telegram/accounts/')) {
        if (unlinkStatus !== 204) {
          return Promise.resolve(
            jsonResponse({ error: 'telegram accounts unavailable' }, unlinkStatus),
          );
        }
        const id = Number(url.split('/').pop());
        rows = rows.filter((row) => (row as { telegramUserId: number }).telegramUserId !== id);
        return Promise.resolve(new Response(null, { status: 204 }));
      }
      return Promise.resolve(jsonResponse({ error: 'unexpected' }, 500));
    }),
  );
  return calls;
}

function renderList() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TelegramLinkedAccounts />
    </QueryClientProvider>,
  );
}

async function confirmUnlinkOf(rowText: string) {
  const row = (await screen.findByText(rowText)).closest('li');
  if (row === null) throw new Error(`no row for ${rowText}`);
  fireEvent.click(within(row).getByRole('button', { name: 'Unlink' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Unlink' }));
}

describe('TelegramLinkedAccounts', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists the linked accounts, oldest first, with their ids and when they were seen', async () => {
    stubServer([PHONE, TABLET]);
    renderList();
    const items = await screen.findAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]?.textContent).toContain('Andrea');
    expect(items[0]?.textContent).toContain('@andrea');
    expect(items[0]?.textContent).toContain('4503599627370495');
    expect(items[0]?.textContent).toContain('last seen');
    expect(items[1]?.textContent).toContain('777');
    expect(items[1]?.textContent).toContain('not seen yet');
  });

  it('says so when no account is linked', async () => {
    stubServer([]);
    renderList();
    expect(await screen.findByText('No Telegram account is linked to this identity.')).toBeTruthy();
  });

  it('unlinks after the confirmation, with that account id, and refetches the list', async () => {
    const calls = stubServer([PHONE, TABLET]);
    renderList();
    await confirmUnlinkOf('Andrea');
    await waitFor(() => {
      expect(screen.queryByText('Andrea')).toBeNull();
    });
    expect(calls).toContain('DELETE /api/settings/telegram/accounts/4503599627370495');
    expect(calls.filter((call) => call === 'GET /api/settings/telegram/accounts')).toHaveLength(2);
    expect(screen.getAllByRole('listitem')).toHaveLength(1);
  });

  it('sends nothing when the confirmation is cancelled', async () => {
    const calls = stubServer([PHONE]);
    renderList();
    const row = (await screen.findByText('Andrea')).closest('li');
    if (row === null) throw new Error('no row');
    fireEvent.click(within(row).getByRole('button', { name: 'Unlink' }));
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }),
    );
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(calls.some((call) => call.startsWith('DELETE'))).toBe(false);
  });

  it('shows why an unlink failed and keeps the row', async () => {
    stubServer([PHONE], 502);
    renderList();
    await confirmUnlinkOf('Andrea');
    expect(
      await screen.findByText("Couldn't unlink the account: telegram accounts unavailable"),
    ).toBeTruthy();
    expect(screen.getByText('Andrea')).toBeTruthy();
  });
});
