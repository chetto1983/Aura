import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../../i18n/i18n';
import type {
  PimAccount,
  PimAuthStatus,
  PimDeviceStart,
  PimGoogleStart,
} from '../../../governance/pimApi';
import { HttpError } from '../../../api/json';
import { chooseOption, optionNames } from '../../../test/chooseOption';

// OtherAccountForm: the full account form behind «Altro tipo di account». It mocks the connect API
// to drive the provider-driven add flow across every connect shape it offers: Google web-redirect,
// Microsoft device-code (with status poll), and a no-auth provider (ICS). IMAP is not one of them:
// a mailbox connects through AddMailAccount, which checks the password before saving it.

const createPimAccount = vi.fn();
const pimGoogleStart = vi.fn();
const pimDeviceStart = vi.fn();
const pimAuthStatus = vi.fn();
const pimAccountLinked = vi.fn();
const listPimProviderApps = vi.fn();

vi.mock('../../../governance/pimApi', async () => {
  const actual = await vi.importActual<typeof import('../../../governance/pimApi')>(
    '../../../governance/pimApi',
  );
  return {
    ...actual,
    listPimProviderApps: (...a: unknown[]) => listPimProviderApps(...a) as Promise<unknown>,
    createPimAccount: (...a: unknown[]) => createPimAccount(...a) as Promise<unknown>,
    pimGoogleStart: (...a: unknown[]) => pimGoogleStart(...a) as Promise<unknown>,
    pimDeviceStart: (...a: unknown[]) => pimDeviceStart(...a) as Promise<unknown>,
    pimAuthStatus: (...a: unknown[]) => pimAuthStatus(...a) as Promise<unknown>,
    pimAccountLinked: (...a: unknown[]) => pimAccountLinked(...a) as Promise<unknown>,
  };
});

const { OtherAccountForm } = await import('../OtherAccountForm');

// The provider apps arrive asynchronously; until they do, a managed provider's «Create account»
// stays disabled, so every case waits for the default (Google, configured) to enable it.
async function renderForm() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const onCreated = vi.fn();
  render(
    <QueryClientProvider client={qc}>
      <OtherAccountForm onCreated={onCreated} />
    </QueryClientProvider>,
  );
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Create account' })).toHaveProperty(
      'disabled',
      false,
    );
  });
  return onCreated;
}

const ACCOUNT: PimAccount = {
  id: 'work',
  displayName: 'Work calendar',
  provider: 'google',
  enabled: true,
  linked: true,
};

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

function fillField(label: RegExp, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

function selectProvider(label: string) {
  return chooseOption('Provider', label);
}

describe('OtherAccountForm', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    pimAccountLinked.mockResolvedValue(false);
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

  it('validates empty Google fields and does not submit', async () => {
    await renderForm();

    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    // accountId + displayName = 2 required errors (the OAuth client is admin-set), no create call.
    await waitFor(() => {
      expect(screen.getAllByText('Required.').length).toBeGreaterThanOrEqual(2);
    });
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  // A mailbox has one path, AddMailAccount's verified one; this form would save an IMAP password
  // unchecked, so it does not offer IMAP at all.
  it('never offers IMAP', async () => {
    await renderForm();

    const options = await optionNames('Provider');
    expect(options).not.toContain('IMAP / SMTP');
    expect(options).toEqual(['Google', 'Microsoft 365', 'Outlook.com', 'ICS feed', 'JSON file']);
  });

  // Measured live 2026-08-22: the sidecar rejects any id outside `^[a-z0-9][a-z0-9\-_]*$` with a
  // 400 whose sentence the cockpit threw away, so an operator typing a name or an email saw only
  // "HTTP 400". The wizard now folds case as they type and refuses the rest before sending.
  it('folds the account id to lowercase as the operator types', async () => {
    await renderForm();

    fillField(/Account ID/i, 'Davide');
    expect(screen.getByLabelText(/Account ID/i).getAttribute('value')).toBe('davide');
  });

  it('refuses a non-slug account id without calling the API, and names the rule', async () => {
    await renderForm();

    fillField(/Account ID/i, 'dvd@gmail.com');
    fillField(/Display name/i, 'Personale');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByText(/starting with a letter or a digit/i)).toBeTruthy();
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('shows the server reason when creation is refused, not a generic error', async () => {
    createPimAccount.mockRejectedValue(
      new HttpError(400, "ProviderConfig is missing required key 'ClientSecret'."),
    );
    await renderForm();

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByText(/missing required key 'ClientSecret'/i)).toBeTruthy();
  });

  it('Google: create → google/start shows the Connect Google link, never the redirect URI', async () => {
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    await renderForm();

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
    createPimAccount.mockResolvedValue({ ...ACCOUNT, id: 'tenant__work' });
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    await renderForm();

    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work calendar');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(pimGoogleStart).toHaveBeenCalledWith('tenant__work');
    });
  });

  it('Google: polls the created account and confirms once it is linked', async () => {
    createPimAccount.mockResolvedValue({ ...ACCOUNT, id: 'tenant__work' });
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    pimAccountLinked.mockResolvedValue(true);
    await renderForm();

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
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockResolvedValue(GOOGLE_START);
    pimAccountLinked.mockResolvedValue(false);
    await renderForm();

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
    createPimAccount.mockResolvedValue({ ...ACCOUNT, id: 'ms', provider: 'microsoft365' });
    pimDeviceStart.mockResolvedValue(DEVICE_START);
    const status: PimAuthStatus = { status: 'completed', message: 'ok' };
    pimAuthStatus.mockResolvedValue(status);
    await renderForm();

    await selectProvider('Microsoft 365');
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

  it('ICS: no-auth provider creates and shows the ready note without any connect flow', async () => {
    createPimAccount.mockResolvedValue({ ...ACCOUNT, provider: 'ics' });
    const created = await renderForm();

    await selectProvider('ICS feed');
    fillField(/Account ID/i, 'holidays');
    fillField(/Display name/i, 'Holidays');
    fillField(/ICS URL/i, 'https://example.com/holidays.ics');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith({
        id: 'holidays',
        displayName: 'Holidays',
        provider: 'ics',
        providerConfig: { icsUrl: 'https://example.com/holidays.ics' },
      });
    });
    expect(await screen.findByText(/ready to use/i)).toBeTruthy();
    expect(created).toHaveBeenCalledOnce();
    expect(pimGoogleStart).not.toHaveBeenCalled();
    expect(pimDeviceStart).not.toHaveBeenCalled();
  });

  it('JSON: offers a OneDrive source only, never a local file path', async () => {
    await renderForm();

    await selectProvider('JSON file');
    expect(await optionNames('Source')).toEqual(['OneDrive']);
    expect(screen.getByLabelText(/OneDrive path/i)).toBeTruthy();
    expect(screen.queryByLabelText(/File path/i)).toBeNull();
  });

  it('create OK but start fails → shows retry (not the create-error banner), retry recovers', async () => {
    createPimAccount.mockResolvedValue(ACCOUNT);
    pimGoogleStart.mockRejectedValueOnce(new Error('HTTP 500')).mockResolvedValueOnce(GOOGLE_START);
    await renderForm();

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
    createPimAccount.mockRejectedValue(new Error('HTTP 409'));
    await renderForm();

    fillField(/Account ID/i, 'dup');
    fillField(/Display name/i, 'Dup');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(pimGoogleStart).not.toHaveBeenCalled();
  });

  it('shows no credential fields for a managed provider', async () => {
    await renderForm();
    expect(screen.queryByLabelText(/^Client ID/i)).toBeNull();
    expect(screen.queryByLabelText(/^Client secret/i)).toBeNull();
  });

  it('blocks an unconfigured managed provider with a note', async () => {
    await renderForm();
    await selectProvider('Outlook.com');
    expect(await screen.findByText(/administrator has to configure Outlook\.com/i)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Create account' })).toHaveProperty('disabled', true);
  });

  it('translates the provider_not_configured 409', async () => {
    createPimAccount.mockRejectedValue(new HttpError(409, 'provider_not_configured'));
    await renderForm();
    fillField(/Account ID/i, 'work');
    fillField(/Display name/i, 'Work');
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));
    expect((await screen.findByRole('alert')).textContent).toMatch(
      /administrator has to configure Google/i,
    );
  });
});
