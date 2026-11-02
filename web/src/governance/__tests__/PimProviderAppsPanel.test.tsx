import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { HttpError } from '../../api/json';
import { chooseOption, optionNames } from '../../test/chooseOption';

const listPimProviderApps = vi.fn();
const savePimProviderApp = vi.fn();

vi.mock('../pimApi', async () => {
  const actual = await vi.importActual<typeof import('../pimApi')>('../pimApi');
  return {
    PIM_PROVIDERS_KEY: actual.PIM_PROVIDERS_KEY,
    listPimProviderApps: (...a: unknown[]) => listPimProviderApps(...a) as Promise<unknown>,
    savePimProviderApp: (...a: unknown[]) => savePimProviderApp(...a) as Promise<unknown>,
  };
});

const { PimProviderAppsPanel } = await import('../PimProviderAppsPanel');

const RELAY = 'https://chetto1983-aura-connect.pages.dev/google/callback/';

function renderPanel() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <PimProviderAppsPanel />
    </QueryClientProvider>,
  );
}

/** Picks a provider by the name its option starts with; the rest of the label is its status. */
function choose(provider: string | RegExp) {
  return chooseOption('Provider', provider);
}

/** The provider the panel shows, with its status. */
async function shownProvider() {
  return (await screen.findByRole('combobox', { name: 'Provider' })).textContent;
}

function configured(provider: string) {
  return { provider, configured: true, clientId: 'cid', tenantId: 't', secretSet: false };
}

beforeEach(() => {
  listPimProviderApps.mockReset();
  savePimProviderApp.mockReset();
  listPimProviderApps.mockResolvedValue([
    {
      provider: 'google',
      configured: true,
      clientId: 'g-cid',
      tenantId: '',
      secretSet: true,
      redirectUri: RELAY,
    },
    {
      provider: 'microsoft365',
      configured: false,
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

describe('PimProviderAppsPanel', () => {
  it('stays collapsed once every managed provider is configured', async () => {
    listPimProviderApps.mockResolvedValue([
      { ...configured('google'), tenantId: '', secretSet: true, redirectUri: RELAY },
      configured('microsoft365'),
      configured('outlook.com'),
    ]);
    renderPanel();
    const summary = await screen.findByText('3 of 3 configured');
    expect(summary.closest('details')?.open).toBe(false);
  });

  it('opens by itself while a provider still needs its app', async () => {
    renderPanel();
    const summary = await screen.findByText('1 of 3 configured');
    expect(summary.closest('details')?.open).toBe(true);
  });

  it('shows one form at a time, starting on the first provider still missing its app', async () => {
    renderPanel();
    expect(await shownProvider()).toBe('Microsoft 365 · not configured');
    expect(screen.getAllByLabelText(/client id/i)).toHaveLength(1);
    const options = await optionNames('Provider');
    expect(options).toContain('Google · configured');
    expect(options).toContain('Outlook.com · not configured');
    expect(screen.queryByText(RELAY)).toBeNull();
  });

  it('keeps the panel open on the saved provider when that save completes the set', async () => {
    const saved = { ...configured('outlook.com'), tenantId: 'consumers' };
    listPimProviderApps
      .mockResolvedValueOnce([
        { ...configured('google'), tenantId: '', secretSet: true, redirectUri: RELAY },
        configured('microsoft365'),
        { ...saved, configured: false, clientId: '', tenantId: '' },
      ])
      .mockResolvedValue([
        { ...configured('google'), tenantId: '', secretSet: true, redirectUri: RELAY },
        configured('microsoft365'),
        saved,
      ]);
    savePimProviderApp.mockResolvedValueOnce(saved);
    renderPanel();
    expect(await shownProvider()).toBe('Outlook.com · not configured');
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    const summary = await screen.findByText('3 of 3 configured');
    expect(summary.closest('details')?.open).toBe(true);
    expect(await shownProvider()).toBe('Outlook.com · configured');
    expect(screen.getByText('Saved.')).toBeTruthy();
  });

  it('pre-fills the client ID, never the secret, and shows the relay URI under Google', async () => {
    renderPanel();
    await choose(/^Google/);
    expect(screen.getByLabelText(/client id/i)).toHaveProperty('value', 'g-cid');
    expect(screen.getByLabelText(/^Client secret/i)).toHaveProperty('value', '');
    expect(screen.getByText(RELAY)).toBeTruthy();
    expect(screen.getByText(/secret is stored/i)).toBeTruthy();
  });

  it('saves Google without a secret when the client ID is unchanged', async () => {
    savePimProviderApp.mockResolvedValueOnce({
      provider: 'google',
      configured: true,
      clientId: 'g-cid',
      tenantId: '',
      secretSet: true,
      redirectUri: RELAY,
    });
    renderPanel();
    await choose(/^Google/);
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('google', { clientId: 'g-cid' });
    });
    expect(await screen.findByText('Saved.')).toBeTruthy();
  });

  it('sends the tenant for Microsoft 365 and shows the server reason on a 400', async () => {
    savePimProviderApp.mockRejectedValueOnce(new HttpError(400, 'tenantId must be a directory ID'));
    renderPanel();
    await choose(/^Microsoft 365/);
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: 'ms-cid' } });
    fireEvent.change(screen.getByLabelText(/tenant id/i), { target: { value: 'consumer' } });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('microsoft365', {
        clientId: 'ms-cid',
        tenantId: 'consumer',
      });
    });
    expect((await screen.findByRole('alert')).textContent).toMatch(/tenantId must be/);
  });

  it('asks Outlook.com for its client alone', async () => {
    savePimProviderApp.mockResolvedValueOnce({ ...configured('outlook.com'), clientId: 'ms-cid' });
    renderPanel();
    await choose(/^Outlook\.com/);
    expect(screen.queryByLabelText(/tenant id/i)).toBeNull();
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: 'ms-cid' } });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('outlook.com', { clientId: 'ms-cid' });
    });
  });

  it('sends a typed secret for Google', async () => {
    savePimProviderApp.mockResolvedValueOnce({
      provider: 'google',
      configured: true,
      clientId: 'new-cid',
      tenantId: '',
      secretSet: true,
      redirectUri: RELAY,
    });
    renderPanel();
    await choose(/^Google/);
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: 'new-cid' } });
    fireEvent.change(screen.getByLabelText(/^Client secret/i), {
      target: { value: 'new-secret' },
    });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('google', {
        clientId: 'new-cid',
        clientSecret: 'new-secret',
      });
    });
  });
});
