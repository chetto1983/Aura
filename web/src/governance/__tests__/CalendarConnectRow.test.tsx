import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { PimAccount, PimDeviceStart, PimGoogleStart } from '../pimApi';
import { mailWatchServer } from '../../test/mailWatchServer';

// CalendarConnectRow test — one account's row in the calendar/PIM connect section: finishing or
// redoing a sign-in on the same account, and removing it only once confirmed, since removing forgets
// what the account's mail watch decided and learned.

const listPimAccounts = vi.fn();
const createPimAccount = vi.fn();
const deletePimAccount = vi.fn();
const pimGoogleStart = vi.fn();
const pimDeviceStart = vi.fn();
const pimAuthStatus = vi.fn();
const pimAccountLinked = vi.fn();
const listPimProviderApps = vi.fn();

vi.mock('../../admin/useAdmin', () => ({ useCapabilities: () => ({ isAdmin: false }) }));

vi.mock('../pimApi', async () => {
  const actual = await vi.importActual<typeof import('../pimApi')>('../pimApi');
  return {
    isCalendarServer: actual.isCalendarServer,
    PIM_PROVIDERS_KEY: actual.PIM_PROVIDERS_KEY,
    PIM_PROVIDER_NOT_CONFIGURED: actual.PIM_PROVIDER_NOT_CONFIGURED,
    listPimProviderApps: (...a: unknown[]) => listPimProviderApps(...a) as Promise<unknown>,
    savePimProviderApp: vi.fn(),
    listPimAccounts: (...a: unknown[]) => listPimAccounts(...a) as Promise<unknown>,
    createPimAccount: (...a: unknown[]) => createPimAccount(...a) as Promise<unknown>,
    deletePimAccount: (...a: unknown[]) => deletePimAccount(...a) as Promise<void>,
    pimGoogleStart: (...a: unknown[]) => pimGoogleStart(...a) as Promise<unknown>,
    pimDeviceStart: (...a: unknown[]) => pimDeviceStart(...a) as Promise<unknown>,
    pimAuthStatus: (...a: unknown[]) => pimAuthStatus(...a) as Promise<unknown>,
    pimAccountLinked: (...a: unknown[]) => pimAccountLinked(...a) as Promise<unknown>,
  };
});

const { CalendarConnect } = await import('../CalendarConnect');

function renderConnect() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  render(<CalendarConnect />, { wrapper: Wrapper });
}

const ACCOUNT: PimAccount = {
  id: 'work',
  displayName: 'Work calendar',
  provider: 'google',
  enabled: true,
  linked: true,
};
const UNFINISHED: PimAccount = { ...ACCOUNT, provider: 'outlook.com', linked: false };
const GOOGLE_START: PimGoogleStart = {
  authUrl: 'https://accounts.google.com/o/oauth2/v2/auth?client_id=x',
  redirectUri: 'https://chetto1983-aura-connect.pages.dev/google/callback/',
};
const DEVICE_START: PimDeviceStart = {
  userCode: 'ABCD-EFGH',
  verificationUrl: 'https://microsoft.com/devicelogin',
  message: 'enter the code',
  expiresIn: 900,
};

describe('CalendarConnect account row', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    pimAccountLinked.mockResolvedValue(false);
    listPimProviderApps.mockResolvedValue([]);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the configured accounts list with provider · id', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    renderConnect();

    expect(await screen.findByText('Work calendar')).toBeTruthy();
    expect(screen.getByText('google · work')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Sign in again' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Remove' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Disconnect' })).toBeNull();
  });

  it('signs a connected Google account in again on the same account, removing nothing', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    pimAccountLinked.mockResolvedValue(true);
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Sign in again' }));

    expect(await screen.findByRole('link', { name: /Connect Google/ })).toBeTruthy();
    expect(pimGoogleStart).toHaveBeenCalledWith('work');
    expect(deletePimAccount).not.toHaveBeenCalled();
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('signs a connected Microsoft account in again with a new code, removing nothing', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [{ ...UNFINISHED, linked: true }] });
    pimDeviceStart.mockResolvedValue(DEVICE_START);
    pimAuthStatus.mockResolvedValue({ status: 'awaiting_user', message: '' });
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Sign in again' }));

    expect(await screen.findByText('ABCD-EFGH')).toBeTruthy();
    expect(pimDeviceStart).toHaveBeenCalledWith('work');
    expect(deletePimAccount).not.toHaveBeenCalled();
  });

  it('says nothing about sign-in on a connected account', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    renderConnect();

    await screen.findByText('Work calendar');
    expect(screen.queryByText(/Sign-in not finished/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Finish sign-in' })).toBeNull();
  });

  it('does not show an account whose sign-in never finished as connected', async () => {
    mailWatchServer();
    listPimAccounts.mockResolvedValue({ accounts: [{ ...UNFINISHED, id: 'home' }] });
    renderConnect();

    expect(await screen.findByText(/Sign-in not finished/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Remove' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Sign in again' })).toBeNull();
    await screen.findByText(/Digest at 18:00/);
    expect(screen.queryByRole('switch', { name: /Mail and calendar watch for/ })).toBeNull();
    vi.unstubAllGlobals();
  });

  it('Remove on an unfinished account fires the delete mutation once confirmed', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [UNFINISHED] });
    deletePimAccount.mockResolvedValue(undefined);
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove for good' }));
    await waitFor(() => {
      expect(deletePimAccount).toHaveBeenCalledWith('work');
    });
  });

  it('finishes a Microsoft sign-in on the same account, with a new code', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [UNFINISHED] });
    pimDeviceStart.mockResolvedValue(DEVICE_START);
    pimAuthStatus.mockResolvedValue({ status: 'awaiting_user', message: '' });
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Finish sign-in' }));

    expect(await screen.findByText('ABCD-EFGH')).toBeTruthy();
    expect(pimDeviceStart).toHaveBeenCalledWith('work');
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('finishes a Google sign-in on the same account', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [{ ...UNFINISHED, provider: 'google' }] });
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Finish sign-in' }));

    await waitFor(() => {
      expect(pimGoogleStart).toHaveBeenCalledWith('work');
    });
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('lets a failed sign-in start be tried again from the row', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [UNFINISHED] });
    pimDeviceStart.mockRejectedValueOnce(new Error('HTTP 502'));
    pimDeviceStart.mockResolvedValue(DEVICE_START);
    pimAuthStatus.mockResolvedValue({ status: 'awaiting_user', message: '' });
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Finish sign-in' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Retry sign-in' }));

    expect(await screen.findByText('ABCD-EFGH')).toBeTruthy();
  });

  it('offers no sign-in on a provider with nothing to sign in to', async () => {
    listPimAccounts.mockResolvedValue({
      accounts: [{ ...ACCOUNT, provider: 'imap', linked: null }],
    });
    renderConnect();

    await screen.findByText('Work calendar');
    expect(screen.queryByRole('button', { name: 'Finish sign-in' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Sign in again' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Remove' })).toBeTruthy();
  });

  it('Remove asks first, and says what the watch loses', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    deletePimAccount.mockResolvedValue(undefined);
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));

    expect(screen.getByText(/decided and learned/)).toBeTruthy();
    expect(deletePimAccount).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Remove for good' }));
    await waitFor(() => {
      expect(deletePimAccount).toHaveBeenCalledWith('work');
    });
  });

  it('Cancel keeps the account and its watch', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    renderConnect();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(screen.queryByText(/decided and learned/)).toBeNull();
    expect(deletePimAccount).not.toHaveBeenCalled();
  });
});
