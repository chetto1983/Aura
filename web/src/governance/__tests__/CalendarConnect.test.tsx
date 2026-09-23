import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { PimAccount, PimAuthStatus, PimDeviceStart, PimGoogleStart } from '../pimApi';
import { HttpError } from '../../api/json';

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
const pimAccountLinked = vi.fn();
const listPimProviderApps = vi.fn();
let caps = { isAdmin: false };

vi.mock('../../admin/useAdmin', () => ({ useCapabilities: () => caps }));

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
  redirectUri: 'https://chetto1983.github.io/aura-connect/google/callback/',
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
    pimAccountLinked.mockReset();
    pimAccountLinked.mockResolvedValue(false);
    caps = { isAdmin: false };
    listPimProviderApps.mockReset();
    listPimProviderApps.mockResolvedValue([
      {
        provider: 'google',
        configured: true,
        clientId: '',
        tenantId: '',
        secretSet: false,
        redirectUri: '',
      },
      {
        provider: 'microsoft365',
        configured: true,
        clientId: '',
        tenantId: '',
        secretSet: false,
        redirectUri: '',
      },
      {
        provider: 'outlook.com',
        configured: false,
        clientId: '',
        tenantId: '',
        secretSet: false,
        redirectUri: '',
      },
    ]);
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
    expect(await screen.findByText(/not configured on this deployment/i)).toBeTruthy();
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

    // accountId + displayName = 2 required errors (the OAuth client is admin-set), no create call.
    await waitFor(() => {
      expect(screen.getAllByText('Required.').length).toBeGreaterThanOrEqual(2);
    });
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('reveals and hides provider secret fields on explicit request', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    selectProvider('imap');
    const password = screen.getByLabelText(/^Password/i);
    expect(password.getAttribute('type')).toBe('password');

    fireEvent.click(screen.getByRole('button', { name: 'Show Password' }));
    expect(password.getAttribute('type')).toBe('text');

    fireEvent.click(screen.getByRole('button', { name: 'Hide Password' }));
    expect(password.getAttribute('type')).toBe('password');
  });

  // Measured live 2026-08-22: the sidecar rejects any id outside `^[a-z0-9][a-z0-9\-_]*$` with a
  // 400 whose sentence the cockpit threw away, so an operator typing a name or an email saw only
  // "HTTP 400". The wizard now folds case as they type and refuses the rest before sending.
  it('folds the account id to lowercase as the operator types', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'Davide');
    expect(screen.getByLabelText(/Account ID/i).getAttribute('value')).toBe('davide');
  });

  it('refuses a non-slug account id without calling the API, and names the rule', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'dvd@gmail.com');
    fillField(/Display name/i, 'Personale');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByText(/starting with a letter or a digit/i)).toBeTruthy();
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('shows the server reason when creation is refused, not a generic error', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockRejectedValue(
      new HttpError(400, "ProviderConfig is missing required key 'ClientSecret'."),
    );
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByText(/missing required key 'ClientSecret'/i)).toBeTruthy();
  });

  it('Google: create → google/start shows the Connect Google link, never the redirect URI', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith({
        id: 'work',
        displayName: 'Work calendar',
        provider: 'google',
        providerConfig: {},
      });
    });
    const link = await screen.findByRole('link', { name: 'Connect Google' });
    expect(screen.queryByText(GOOGLE_START.redirectUri)).toBeNull();
    expect(link.getAttribute('href')).toBe(GOOGLE_START.authUrl);
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
  });

  it('starts provider authentication with the canonical account id returned by create', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue({ ...ACCOUNT, id: 'tenant__work' });
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(pimGoogleStart).toHaveBeenCalledWith('tenant__work');
    });
  });

  it('Google: polls the created account and confirms once it is linked', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue({ ...ACCOUNT, id: 'tenant__work' });
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    pimAccountLinked.mockResolvedValue(true);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(
      await screen.findByText('Google account linked. You can close the Google tab.'),
    ).toBeTruthy();
    expect(pimAccountLinked).toHaveBeenCalledWith('tenant__work');
    expect(screen.queryByRole('link', { name: 'Connect Google' })).toBeNull();
    expect(screen.queryByText(GOOGLE_START.redirectUri)).toBeNull();
  });

  it('Google: keeps the consent panel while the account is not linked yet', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    pimAccountLinked.mockResolvedValue(false);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(pimAccountLinked).toHaveBeenCalledWith('work');
    });
    expect(screen.getByRole('link', { name: 'Connect Google' })).toBeTruthy();
    expect(screen.queryByText('Google account linked. You can close the Google tab.')).toBeNull();
  });

  it('Microsoft: switching provider swaps fields and runs the device-code flow', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue({ ...ACCOUNT, id: 'ms', provider: 'microsoft365' });
    pimDeviceStart.mockResolvedValue(DEVICE_START);
    const status: PimAuthStatus = { status: 'completed', message: 'ok' };
    pimAuthStatus.mockResolvedValue(status);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    selectProvider('microsoft365');
    // The tenant and client are admin-set: the member types neither.
    expect(screen.queryByLabelText(/Tenant ID/i)).toBeNull();
    expect(screen.queryByLabelText(/^Client ID/i)).toBeNull();
    fillField(/Account ID/i, 'ms');
    fillField(/Display name/i, 'MS work');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith({
        id: 'ms',
        displayName: 'MS work',
        provider: 'microsoft365',
        providerConfig: {},
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

  it('create OK but start fails → shows retry (not the create-error banner), retry recovers', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockRejectedValueOnce(new Error('HTTP 500')).mockResolvedValueOnce(GOOGLE_START);
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    // The account WAS created; the connect step failed → recoverable "Retry sign-in", NOT the
    // generic create-failure banner.
    const retry = await screen.findByRole('button', { name: 'Retry sign-in' });
    expect(createPimAccount).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('governance.error')).toBeNull();

    fireEvent.click(retry);
    // Retry re-runs ONLY the start (no second createPimAccount → no 409 dead-end).
    expect(await screen.findByRole('link', { name: 'Connect Google' })).toBeTruthy();
    expect(createPimAccount).toHaveBeenCalledTimes(1);
  });

  it('renders the generic error when create fails', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockRejectedValue(new Error('HTTP 409'));
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);

    fillField(/Account ID/i, 'dup');
    fillField(/Display name/i, 'Dup');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(pimGoogleStart).not.toHaveBeenCalled();
  });

  it('hides the provider-app panel from members', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);
    expect(screen.queryByText(/provider oauth apps/i)).toBeNull();
  });

  it('shows the provider-app panel to an admin', async () => {
    caps = { isAdmin: true };
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    expect(await screen.findByText(/provider oauth apps/i)).toBeTruthy();
  });

  it('shows no credential fields for a managed provider', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);
    expect(screen.queryByLabelText(/^Client ID/i)).toBeNull();
    expect(screen.queryByLabelText(/^Client secret/i)).toBeNull();
  });

  it('blocks an unconfigured managed provider with a note', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);
    selectProvider('outlook.com');
    expect(await screen.findByText(/administrator has to configure Outlook\.com/i)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Create account' })).toHaveProperty('disabled', true);
  });

  it('translates the provider_not_configured 409', async () => {
    listPimAccounts.mockResolvedValue({ accounts: [] });
    createPimAccount.mockRejectedValue(new HttpError(409, 'provider_not_configured'));
    renderConnect();
    await screen.findByText(/No calendar accounts yet/i);
    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));
    expect((await screen.findByRole('alert')).textContent).toMatch(
      /administrator has to configure Google/i,
    );
  });
});
