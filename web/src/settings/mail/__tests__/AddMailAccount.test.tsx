import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import i18n from '../../../i18n/i18n';
import { HttpError } from '../../../api/json';
import type { PimAccount, PimDiscovery } from '../../../governance/pimApi';

// AddMailAccount: the address-and-password connect screen. The PIM is mocked at the pimApi seam;
// the pure helpers (pimVerifyFailure, pimErrorKey) run for real, so a 422 body is read the way the
// relay sends it.

const discoverPim = vi.fn();
const createPimAccount = vi.fn();
const listPimAccounts = vi.fn();
const deletePimAccount = vi.fn();
const patchPimProviderConfig = vi.fn();
const pimGoogleStart = vi.fn();
const pimDeviceStart = vi.fn();
const pimAccountLinked = vi.fn();
const listPimProviderApps = vi.fn();

vi.mock('../../../governance/pimApi', async () => {
  const actual = await vi.importActual<typeof import('../../../governance/pimApi')>(
    '../../../governance/pimApi',
  );
  return {
    PIM_PROVIDERS_KEY: actual.PIM_PROVIDERS_KEY,
    PIM_PROVIDER_NOT_CONFIGURED: actual.PIM_PROVIDER_NOT_CONFIGURED,
    pimVerifyFailure: actual.pimVerifyFailure,
    pimErrorKey: actual.pimErrorKey,
    discoverPim: (...a: unknown[]) => discoverPim(...a) as Promise<unknown>,
    createPimAccount: (...a: unknown[]) => createPimAccount(...a) as Promise<unknown>,
    listPimAccounts: (...a: unknown[]) => listPimAccounts(...a) as Promise<unknown>,
    deletePimAccount: (...a: unknown[]) => deletePimAccount(...a) as Promise<unknown>,
    patchPimProviderConfig: (...a: unknown[]) => patchPimProviderConfig(...a) as Promise<unknown>,
    pimGoogleStart: (...a: unknown[]) => pimGoogleStart(...a) as Promise<unknown>,
    pimDeviceStart: (...a: unknown[]) => pimDeviceStart(...a) as Promise<unknown>,
    pimAuthStatus: vi.fn(),
    pimAccountLinked: (...a: unknown[]) => pimAccountLinked(...a) as Promise<unknown>,
    listPimProviderApps: (...a: unknown[]) => listPimProviderApps(...a) as Promise<unknown>,
    savePimProviderApp: vi.fn(),
  };
});

const { AddMailAccount } = await import('../AddMailAccount');

const LIBERO: PimDiscovery = {
  kind: 'imap',
  source: 'preset',
  imapHost: 'imapmail.libero.it',
  imapPort: 993,
  imapSecurity: 'ssl',
  smtpHost: 'smtp.libero.it',
  smtpPort: 465,
  smtpSecurity: 'ssl',
  username: 'mario@libero.it',
};

const GMAIL: PimDiscovery = {
  kind: 'google',
  source: 'preset',
  imapHost: 'imap.gmail.com',
  imapPort: 993,
  imapSecurity: 'ssl',
  smtpHost: 'smtp.gmail.com',
  smtpPort: 465,
  smtpSecurity: 'ssl',
  username: 'mario@gmail.com',
  appPassword: { required: true, helpUrl: 'https://support.google.com/accounts/answer/185833' },
  passwordFallback: true,
};

function account(over: Partial<PimAccount> = {}): PimAccount {
  return {
    id: 'mario-libero-it',
    displayName: 'mario@libero.it',
    provider: 'imap',
    enabled: true,
    linked: null,
    ...over,
  };
}

function verifyFailed(step: 'imap' | 'smtp', reason: string): HttpError {
  return new HttpError(422, 'verify_failed', '', { error: 'verify_failed', step, reason });
}

function renderAdd() {
  const onCreated = vi.fn();
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(<AddMailAccount onCreated={onCreated} />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  return onCreated;
}

function addressField(): HTMLElement {
  return screen.getByLabelText('Indirizzo email');
}

function passwordField(): HTMLElement {
  return screen.getByLabelText('Password');
}

async function discover(address: string, answer: PimDiscovery | Error) {
  if (answer instanceof Error) discoverPim.mockRejectedValueOnce(answer);
  else discoverPim.mockResolvedValueOnce(answer);
  const before = discoverPim.mock.calls.length;
  fireEvent.change(addressField(), { target: { value: address } });
  fireEvent.blur(addressField());
  await waitFor(() => {
    expect(discoverPim.mock.calls.slice(before)).toEqual([[address]]);
  });
  await waitFor(() => {
    expect(screen.queryByText(i18n.t('settings.mail.discovering'))).toBeNull();
  });
}

function serverDetails(): HTMLDetailsElement {
  const summary = screen.getByText('Impostazioni server');
  const details = summary.closest('details');
  if (details === null) throw new Error('Impostazioni server is not a <details>');
  return details;
}

function connect() {
  fireEvent.click(screen.getByRole('button', { name: 'Collega' }));
}

beforeEach(async () => {
  await i18n.changeLanguage('it');
  for (const fn of [
    discoverPim,
    createPimAccount,
    listPimAccounts,
    deletePimAccount,
    patchPimProviderConfig,
    pimGoogleStart,
    pimDeviceStart,
    pimAccountLinked,
    listPimProviderApps,
  ]) {
    fn.mockReset();
  }
  pimAccountLinked.mockResolvedValue(false);
  listPimProviderApps.mockResolvedValue([]);
  listPimAccounts.mockResolvedValue({ accounts: [] });
});

afterEach(() => {
  vi.clearAllMocks();
});

afterAll(async () => {
  await i18n.changeLanguage('en');
});

describe('AddMailAccount', () => {
  it('DiscoversOnBlur', async () => {
    renderAdd();
    expect(addressField()).toBeTruthy();
    expect(passwordField()).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Collega' })).toBeTruthy();

    fireEvent.change(addressField(), { target: { value: 'mario@' } });
    fireEvent.blur(addressField());
    expect(discoverPim).not.toHaveBeenCalled();

    fireEvent.change(addressField(), { target: { value: 'mario@libero.it' } });
    expect(discoverPim).not.toHaveBeenCalled();
    await discover('mario@libero.it', LIBERO);

    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>('Host IMAP').value).toBe('imapmail.libero.it');
    });
    expect(screen.getByLabelText<HTMLInputElement>('Porta SMTP').value).toBe('465');
    expect(screen.getByLabelText<HTMLInputElement>('Nome utente').value).toBe('mario@libero.it');
    expect(serverDetails().open).toBe(false);
    expect(within(serverDetails()).queryByLabelText('Password')).toBeNull();
  });

  it('PimDownDuringDiscoveryOpensTheServerFields', async () => {
    renderAdd();
    await discover('mario@example.com', new HttpError(502));

    await waitFor(() => {
      expect(serverDetails().open).toBe(true);
    });
    expect(screen.getByLabelText('Host IMAP').getAttribute('placeholder')).toBe('imap.example.com');
    expect(passwordField()).toBeTruthy();
  });

  it('GmailOffersGoogleSignIn', async () => {
    createPimAccount.mockResolvedValue(
      account({ id: 'mario-gmail-com', displayName: 'mario@gmail.com', provider: 'google' }),
    );
    pimGoogleStart.mockResolvedValue({
      authUrl: 'https://accounts.google.com/o/oauth2/v2/auth?client_id=x',
      redirectUri: 'https://example.invalid/cb',
    });
    const onCreated = renderAdd();
    await discover('mario@gmail.com', GMAIL);

    const signIn = await screen.findByRole('button', { name: 'Accedi con Google' });
    expect(screen.queryByLabelText('Password')).toBeNull();
    expect(createPimAccount).not.toHaveBeenCalled();

    fireEvent.click(signIn);
    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledWith(
        {
          id: 'mario-gmail-com',
          displayName: 'mario@gmail.com',
          provider: 'google',
          providerConfig: {},
        },
        expect.any(AbortSignal),
      );
    });
    expect(await screen.findByRole('link', { name: 'Connetti Google' })).toBeTruthy();
    expect(pimGoogleStart).toHaveBeenCalledWith('mario-gmail-com');
    expect(onCreated).toHaveBeenCalled();
  });

  it('NoGoogleAppOffersAnAppPassword', async () => {
    createPimAccount.mockRejectedValueOnce(new HttpError(409, 'provider_not_configured'));
    renderAdd();
    await discover('mario@gmail.com', GMAIL);
    fireEvent.click(await screen.findByRole('button', { name: 'Accedi con Google' }));

    expect(
      await screen.findByText(i18n.t('settings.mail.providerAppMissing', { provider: 'Google' })),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Usa una password per app' }));

    expect(passwordField()).toBeTruthy();
    expect(screen.getByLabelText<HTMLInputElement>('Host IMAP').value).toBe('imap.gmail.com');
    expect(
      screen.queryByText(i18n.t('settings.mail.providerAppMissing', { provider: 'Google' })),
    ).toBeNull();

    createPimAccount.mockResolvedValueOnce(
      account({ id: 'mario-gmail-com', displayName: 'mario@gmail.com' }),
    );
    fireEvent.change(passwordField(), { target: { value: 'abcd efgh ijkl mnop' } });
    connect();
    await waitFor(() => {
      expect(createPimAccount).toHaveBeenLastCalledWith(
        expect.objectContaining({
          id: 'mario-gmail-com',
          provider: 'imap',
          providerConfig: expect.objectContaining({
            imapHost: 'imap.gmail.com',
            smtpHost: 'smtp.gmail.com',
            password: 'abcd efgh ijkl mnop',
          }) as unknown,
        }),
        expect.any(AbortSignal),
      );
    });
  });

  it('NoMicrosoftAppOffersNoPassword', async () => {
    createPimAccount.mockRejectedValueOnce(new HttpError(409, 'provider_not_configured'));
    renderAdd();
    await discover('mario@outlook.com', { kind: 'outlook.com', passwordFallback: false });
    fireEvent.click(await screen.findByRole('button', { name: 'Accedi con Microsoft' }));

    expect(
      await screen.findByText(
        i18n.t('settings.mail.providerAppMissingNoFallback', { provider: 'Outlook.com' }),
      ),
    ).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Usa una password per app' })).toBeNull();
    expect(screen.queryByLabelText('Password')).toBeNull();
    expect(
      screen.queryByText(i18n.t('settings.mail.providerAppMissing', { provider: 'Outlook.com' })),
    ).toBeNull();
  });

  it('AppPasswordNoticeBeforeTrying', async () => {
    renderAdd();
    await discover('mario@yahoo.com', {
      ...LIBERO,
      imapHost: 'imap.mail.yahoo.com',
      smtpHost: 'smtp.mail.yahoo.com',
      appPassword: { required: true, helpUrl: 'https://help.yahoo.com/kb/SLN15241.html' },
    });

    const notice = await screen.findByText(i18n.t('settings.mail.appPasswordNotice'));
    const link = screen.getByRole('link', { name: i18n.t('settings.mail.appPasswordHelp') });
    expect(link.getAttribute('href')).toBe('https://help.yahoo.com/kb/SLN15241.html');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(
      passwordField().compareDocumentPosition(notice) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('MxDerivedServersAreShownBeforeConnecting', async () => {
    renderAdd();
    await discover('mario@studio.it', {
      ...LIBERO,
      source: 'ispdb-mx',
      imapHost: 'imap.provider.net',
      smtpHost: 'smtp.provider.net',
    });

    const notice = await screen.findByText('Mi collegherò a imap.provider.net');
    const button = screen.getByRole('button', { name: 'Collega' });
    expect(
      passwordField().compareDocumentPosition(notice) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(notice.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('UnknownOpensTheServerFieldsWithPlaceholders', async () => {
    renderAdd();
    await discover('mario@example.com', { kind: 'unknown' });

    await waitFor(() => {
      expect(serverDetails().open).toBe(true);
    });
    const imapHost = screen.getByLabelText<HTMLInputElement>('Host IMAP');
    const smtpHost = screen.getByLabelText<HTMLInputElement>('Host SMTP');
    expect(imapHost.value).toBe('');
    expect(imapHost.getAttribute('placeholder')).toBe('imap.example.com');
    expect(smtpHost.value).toBe('');
    expect(smtpHost.getAttribute('placeholder')).toBe('smtp.example.com');

    fireEvent.change(passwordField(), { target: { value: 'pw' } });
    connect();
    await waitFor(() => {
      expect(imapHost.getAttribute('aria-invalid')).toBe('true');
    });
    expect(createPimAccount).not.toHaveBeenCalled();
  });

  it('WrongPasswordNamesThePasswordField', async () => {
    createPimAccount.mockRejectedValueOnce(verifyFailed('imap', 'auth_failed'));
    renderAdd();
    await discover('mario@libero.it', LIBERO);
    expect(passwordField().getAttribute('aria-invalid')).toBeNull();

    fireEvent.change(passwordField(), { target: { value: 'wrong' } });
    connect();

    const message = await screen.findByText(i18n.t('settings.mail.verify.auth_failed'));
    const password = passwordField();
    expect(password.getAttribute('aria-invalid')).toBe('true');
    expect(password.getAttribute('aria-describedby')?.split(' ')).toContain(message.id);
    expect(serverDetails().open).toBe(false);

    fireEvent.change(password, { target: { value: 'right' } });
    expect(passwordField().getAttribute('aria-invalid')).toBeNull();
    expect(screen.queryByText(i18n.t('settings.mail.verify.auth_failed'))).toBeNull();
  });

  it('SmtpWarningIsShownAfterConnecting', async () => {
    let resolve: (value: unknown) => void = () => undefined;
    createPimAccount.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const onCreated = renderAdd();
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: 'secret' } });
    connect();

    expect(await screen.findByRole('button', { name: "Verifico l'accesso…" })).toBeTruthy();
    expect(createPimAccount).toHaveBeenCalledWith(
      {
        id: 'mario-libero-it',
        displayName: 'mario@libero.it',
        provider: 'imap',
        providerConfig: {
          imapHost: 'imapmail.libero.it',
          imapPort: '993',
          imapSecurity: 'ssl',
          smtpHost: 'smtp.libero.it',
          smtpPort: '465',
          smtpSecurity: 'ssl',
          username: 'mario@libero.it',
          password: 'secret',
        },
      },
      expect.any(AbortSignal),
    );

    resolve({ ...account(), smtpWarning: 'smtp_unreachable' });
    const name = await screen.findByText('mario@libero.it');
    const warning = screen.getByText(i18n.t('settings.mail.verify.smtpWarning.smtp_unreachable'));
    expect(name.compareDocumentPosition(warning) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(onCreated).toHaveBeenCalled();
  });

  it('SmtpAuthFailureOpensTheServerFields', async () => {
    createPimAccount.mockRejectedValueOnce(verifyFailed('smtp', 'smtp_auth_failed'));
    renderAdd();
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: 'secret' } });
    connect();

    const message = await screen.findByText(i18n.t('settings.mail.verify.smtp_auth_failed'));
    expect(serverDetails().open).toBe(true);
    expect(serverDetails().contains(message)).toBe(true);
    expect(passwordField().getAttribute('aria-invalid')).toBeNull();
  });

  it('ConflictOnTheSameAddressOffersRepair', async () => {
    createPimAccount.mockRejectedValue(new HttpError(409));
    listPimAccounts.mockResolvedValue({ accounts: [account()] });
    patchPimProviderConfig.mockResolvedValue(account());
    renderAdd();
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: 'new pass ' } });
    connect();

    expect(await screen.findByText('Questo indirizzo è già collegato')).toBeTruthy();
    expect(createPimAccount).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Cambia password' }));
    await waitFor(() => {
      expect(patchPimProviderConfig).toHaveBeenCalledWith('mario-libero-it', {
        password: 'new pass ',
      });
    });

    // An unfinished OAuth sign-in left under the same address is offered for removal instead.
    await screen.findByText('mario@libero.it');
    listPimAccounts.mockResolvedValue({
      accounts: [account({ provider: 'google', linked: false })],
    });
    deletePimAccount.mockResolvedValue(undefined);
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: 'secret' } });
    connect();
    fireEvent.click(await screen.findByRole('button', { name: 'Rimuovi' }));
    await waitFor(() => {
      expect(deletePimAccount).toHaveBeenCalledWith('mario-libero-it');
    });
  });

  it('ConflictOnAnotherAddressRetriesWithASuffix', async () => {
    listPimAccounts.mockResolvedValue({
      accounts: [account({ displayName: 'Mario Rossi (lavoro)' })],
    });
    createPimAccount
      .mockRejectedValueOnce(new HttpError(409))
      .mockRejectedValueOnce(new HttpError(409))
      .mockResolvedValueOnce(account({ id: 'mario-libero-it-3' }));
    renderAdd();
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: 'secret' } });
    connect();

    await screen.findByText('mario@libero.it');
    expect(createPimAccount.mock.calls.map(([body]) => (body as { id: string }).id)).toEqual([
      'mario-libero-it',
      'mario-libero-it-2',
      'mario-libero-it-3',
    ]);

    // The suffix stops at -9.
    createPimAccount.mockReset();
    createPimAccount.mockRejectedValue(new HttpError(409));
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: 'secret' } });
    connect();
    expect(await screen.findByText(i18n.t('settings.mail.error.generic'))).toBeTruthy();
    expect(createPimAccount).toHaveBeenCalledTimes(9);
    expect(createPimAccount.mock.calls.at(-1)?.[0]).toMatchObject({ id: 'mario-libero-it-9' });
  });

  it('CreateTimeoutChecksTheListBeforeRetrying', async () => {
    createPimAccount.mockRejectedValueOnce(new DOMException('slow', 'TimeoutError'));
    renderAdd();
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: 'secret' } });
    connect();
    expect(await screen.findByText(i18n.t('settings.mail.error.timeout'))).toBeTruthy();

    // Nothing landed: the retry creates again, and times out again.
    createPimAccount.mockRejectedValueOnce(new DOMException('slow', 'TimeoutError'));
    connect();
    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalledTimes(2);
    });
    expect(listPimAccounts).toHaveBeenCalledTimes(1);
    expect(await screen.findByText(i18n.t('settings.mail.error.timeout'))).toBeTruthy();

    // The second one landed: the list says so and nothing is created a third time.
    listPimAccounts.mockResolvedValue({ accounts: [account()] });
    connect();
    await screen.findByText('mario@libero.it');
    expect(listPimAccounts).toHaveBeenCalledTimes(2);
    expect(createPimAccount).toHaveBeenCalledTimes(2);
  });

  it('TrailingSpaceInThePasswordIsSent', async () => {
    createPimAccount.mockResolvedValue(account());
    renderAdd();
    await discover('mario@libero.it', LIBERO);
    fireEvent.change(passwordField(), { target: { value: ' secret  ' } });
    connect();

    await waitFor(() => {
      expect(createPimAccount).toHaveBeenCalled();
    });
    const body = createPimAccount.mock.calls[0]?.[0] as { providerConfig: Record<string, string> };
    expect(body.providerConfig.password).toBe(' secret  ');
  });

  it('OtherAccountTypeShowsTheFullForm', async () => {
    renderAdd();
    expect(screen.queryByRole('combobox', { name: 'Provider' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Altro tipo di account' }));

    expect(await screen.findByRole('combobox', { name: 'Provider' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Crea account' })).toBeTruthy();
  });
});
