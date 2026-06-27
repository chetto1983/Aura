import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { PimAccount, PimAuthStatus, PimDeviceStart, PimGoogleStart } from '../pimApi';

// CalendarConnect test — the cockpit calendar/PIM connect section. It mocks the connect API to drive:
// the accounts list (render + disconnect), the empty/offline states, and the provider-driven add
// flow across every connect shape — Google web-redirect, Microsoft device-code (with status poll),
// and a no-auth credential provider (IMAP). It also covers provider-switching field visibility and
// the isCalendarServer recipe/source/name heuristic.

const listPimAccounts = vi.fn();
const createPimAccount = vi.fn();
const deletePimAccount = vi.fn();
const pimGoogleStart = vi.fn();
const pimDeviceStart = vi.fn();
const pimAuthStatus = vi.fn();

vi.mock('../pimApi', async () => {
  const actual = await vi.importActual<typeof import('../pimApi')>('../pimApi');
  return {
    isCalendarServer: actual.isCalendarServer,
    listPimAccounts: (...a: unknown[]) => listPimAccounts(...a) as Promise<unknown>,
    createPimAccount: (...a: unknown[]) => createPimAccount(...a) as Promise<unknown>,
    deletePimAccount: (...a: unknown[]) => deletePimAccount(...a) as Promise<void>,
    pimGoogleStart: (...a: unknown[]) => pimGoogleStart(...a) as Promise<unknown>,
    pimDeviceStart: (...a: unknown[]) => pimDeviceStart(...a) as Promise<unknown>,
    pimAuthStatus: (...a: unknown[]) => pimAuthStatus(...a) as Promise<unknown>,
  };
});

const { CalendarConnect } = await import('../CalendarConnect');
const { isCalendarServer } = await import('../pimApi');

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

const GOOGLE_START: PimGoogleStart = {
  authUrl: 'https://accounts.google.com/o/oauth2/v2/auth?client_id=x',
  redirectUri: 'http://localhost:8093/admin/auth/google/callback',
};

const DEVICE_START: PimDeviceStart = {
  userCode: 'ABCD-EFGH',
  verificationUrl: 'https://microsoft.com/devicelogin',
  message: 'enter the code',
  expiresIn: 900,
};

function fillField(label: RegExp, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

function selectProvider(value: string) {
  fireEvent.change(screen.getByLabelText('Provider'), { target: { value } });
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
    pimDeviceStart.mockReset();
    pimAuthStatus.mockReset();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the configured accounts list with provider · id', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [ACCOUNT] });
    renderConnect();

    expect(await screen.findByText('Work calendar')).toBeTruthy();
    expect(screen.getByText('google · work')).toBeTruthy();
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

  it('validates empty Google fields and does not submit', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    // accountId + displayName + clientId + clientSecret = 4 required errors, no create call.
    await waitFor(() => {
      expect(screen.getAllByText('Required.').length).toBeGreaterThanOrEqual(4);
    });
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('Google: create → google/start shows the redirect URI + the Connect Google link', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fillField(/^Client ID/i, 'client-id-123');
    fillField(/^Client secret/i, 'client-secret-456');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith({
        id: 'work',
        displayName: 'Work calendar',
        provider: 'google',
        providerConfig: { clientId: 'client-id-123', clientSecret: 'client-secret-456' },
      });
    });
    expect(await screen.findByText(GOOGLE_START.redirectUri)).toBeTruthy();
    const link = screen.getByRole('link', { name: 'Connect Google' });
    expect(link.getAttribute('href')).toBe(GOOGLE_START.authUrl);
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
  });

  it('Microsoft: switching provider swaps fields and runs the device-code flow', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue({ ...ACCOUNT, provider: 'microsoft365' });
    pimDeviceStart.mockResolvedValue(DEVICE_START);
    const status: PimAuthStatus = { status: 'completed', message: 'ok' };
    pimAuthStatus.mockResolvedValue(status);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    selectProvider('microsoft365');
    // Google's clientSecret field is gone; Tenant ID appears.
    expect(screen.queryByLabelText(/^Client secret/i)).toBeNull();
    fillField(/Account ID/i, 'ms');
    fillField(/Display name/i, 'MS work');
    fillField(/Tenant ID/i, 'common');
    fillField(/^Client ID/i, 'ms-client');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith({
        id: 'ms',
        displayName: 'MS work',
        provider: 'microsoft365',
        providerConfig: { tenantId: 'common', clientId: 'ms-client' },
      });
    });
    expect(pimDeviceStart).toHaveBeenCalledWith('ms');
    expect(await screen.findByText('ABCD-EFGH')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open Microsoft sign-in' }).getAttribute('href')).toBe(
      DEVICE_START.verificationUrl,
    );
    expect(await screen.findByText(/Account linked/i)).toBeTruthy();
    expect(pimGoogleStart).not.toHaveBeenCalled();
  });

  it('IMAP: no-auth provider creates and shows the ready note without any connect flow', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue({ ...ACCOUNT, provider: 'imap' });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    selectProvider('imap');
    fillField(/Account ID/i, 'mail');
    fillField(/Display name/i, 'Mailbox');
    fillField(/IMAP host/i, 'imap.example.com');
    fillField(/SMTP host/i, 'smtp.example.com');
    fillField(/Username/i, 'me@example.com');
    fillField(/^Password/i, 's3cret');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith({
        id: 'mail',
        displayName: 'Mailbox',
        provider: 'imap',
        providerConfig: {
          imapHost: 'imap.example.com',
          smtpHost: 'smtp.example.com',
          username: 'me@example.com',
          password: 's3cret',
        },
      });
    });
    expect(await screen.findByText(/ready to use/i)).toBeTruthy();
    expect(pimGoogleStart).not.toHaveBeenCalled();
    expect(pimDeviceStart).not.toHaveBeenCalled();
  });

  it('JSON: source select toggles between filePath and oneDrivePath', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    selectProvider('json');
    expect(screen.getByLabelText(/File path/i)).toBeTruthy();
    expect(screen.queryByLabelText(/OneDrive path/i)).toBeNull();

    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'onedrive' } });
    expect(screen.getByLabelText(/OneDrive path/i)).toBeTruthy();
    expect(screen.queryByLabelText(/File path/i)).toBeNull();
  });

  it('renders the generic error when create fails', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockRejectedValue(new Error('HTTP 409'));
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'dup');
    fillField(/Display name/i, 'Dup');
    fillField(/^Client ID/i, 'cid');
    fillField(/^Client secret/i, 'sec');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(pimGoogleStart).not.toHaveBeenCalled();
  });
});
