import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { PimAccount, PimGoogleStart } from '../governanceApi';

// CalendarConnect test — the cockpit "Connect Google Calendar" section. It mocks the connect API to
// drive: the accounts list (render + disconnect), the empty state, the add-account flow
// (validation → create → google/start → redirectUri + Connect Google link), and the offline/error
// note. It also covers the isCalendarServer recipe/source/name heuristic.

const listPimAccounts = vi.fn();
const createPimAccount = vi.fn();
const deletePimAccount = vi.fn();
const pimGoogleStart = vi.fn();
const pimLogout = vi.fn();

vi.mock('../governanceApi', async () => {
  const actual = await vi.importActual<typeof import('../governanceApi')>('../governanceApi');
  return {
    isCalendarServer: actual.isCalendarServer,
    listPimAccounts: (...a: unknown[]) => listPimAccounts(...a) as Promise<unknown>,
    createPimAccount: (...a: unknown[]) => createPimAccount(...a) as Promise<unknown>,
    deletePimAccount: (...a: unknown[]) => deletePimAccount(...a) as Promise<void>,
    pimGoogleStart: (...a: unknown[]) => pimGoogleStart(...a) as Promise<unknown>,
    pimLogout: (...a: unknown[]) => pimLogout(...a) as Promise<void>,
  };
});

const { CalendarConnect } = await import('../CalendarConnect');
const { isCalendarServer } = await import('../governanceApi');

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function Wrapper({ children, qc }: { children: ReactNode; qc: QueryClient }) {
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderConnect() {
  const qc = client();
  render(<CalendarConnect />, { wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper> });
  return qc;
}

const ACCOUNT: PimAccount = {
  id: 'work',
  displayName: 'Work calendar',
  provider: 'google',
  enabled: true,
};

const START: PimGoogleStart = {
  authUrl: 'https://accounts.google.com/o/oauth2/v2/auth?client_id=x',
  redirectUri: 'http://localhost:8093/admin/auth/google/callback',
};

function fillField(label: RegExp, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

describe('isCalendarServer', () => {
  it('matches by name, recipe source, and pim source substring; rejects others', () => {
    expect(isCalendarServer({ name: 'calendar', source: '' })).toBe(true);
    expect(isCalendarServer({ name: 'cal', source: 'recipe:calendar' })).toBe(true);
    expect(isCalendarServer({ name: 'cal', source: 'ghcr.io/.../aura-pim-mcp' })).toBe(true);
    expect(isCalendarServer({ name: 'cal', source: 'something-pim-thing' })).toBe(true);
    expect(isCalendarServer({ name: 'whatsapp', source: 'recipe:whatsapp' })).toBe(false);
    expect(isCalendarServer({ name: 'github', source: 'manual' })).toBe(false);
  });
});

describe('CalendarConnect', () => {
  beforeEach(() => {
    listPimAccounts.mockReset();
    createPimAccount.mockReset();
    deletePimAccount.mockReset();
    pimGoogleStart.mockReset();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the configured accounts list', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    renderConnect();

    expect(await screen.findByText('Work calendar')).toBeTruthy();
    expect(screen.getByText('work')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Disconnect' })).toBeTruthy();
  });

  it('shows the empty note when there are no accounts', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();

    expect(await screen.findByText(/No calendar accounts yet/i)).toBeTruthy();
  });

  it('shows the offline note when the accounts query errors (503 sidecar unconfigured)', async () => {
    listPimAccounts.mockRejectedValue(new Error('HTTP 503'));
    renderConnect();

    expect(await screen.findByText(/isn't configured on this deployment/i)).toBeTruthy();
  });

  it('Disconnect fires the delete mutation', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    deletePimAccount.mockResolvedValue(undefined);
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Disconnect' }));
    await waitFor(() => {
      expect(deletePimAccount).toHaveBeenCalledWith('work');
    });
  });

  it('validates empty fields and does not submit', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    // Four required-field errors, no create call.
    await waitFor(() => {
      expect(screen.getAllByText('Required.').length).toBeGreaterThanOrEqual(4);
    });
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('create → google/start shows the redirect URI + the Connect Google link', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockResolvedValue(START);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fillField(/Google client ID/i, 'client-id-123');
    fillField(/Google client secret/i, 'client-secret-456');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith({
        id: 'work',
        displayName: 'Work calendar',
        provider: 'google',
        providerConfig: { clientId: 'client-id-123', clientSecret: 'client-secret-456' },
      });
    });
    expect(await screen.findByText(START.redirectUri)).toBeTruthy();
    const link = screen.getByRole('link', { name: 'Connect Google' });
    expect(link.getAttribute('href')).toBe(START.authUrl);
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(screen.getByText(/Complete consent in the opened tab/i)).toBeTruthy();
  });

  it('renders the generic error when create fails', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockRejectedValue(new Error('HTTP 409'));
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'dup');
    fillField(/Display name/i, 'Dup');
    fillField(/Google client ID/i, 'cid');
    fillField(/Google client secret/i, 'sec');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(pimGoogleStart).not.toHaveBeenCalled();
  });
});
