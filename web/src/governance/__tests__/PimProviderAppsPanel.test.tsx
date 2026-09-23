import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { HttpError } from '../../api/json';

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

const RELAY = 'https://chetto1983.github.io/aura-connect/google/callback/';

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

async function choose(provider: string) {
  fireEvent.change(await screen.findByLabelText('Provider'), { target: { value: provider } });
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
    expect(await screen.findByLabelText('Provider')).toHaveProperty('value', 'microsoft365');
    expect(screen.getAllByLabelText(/client id/i)).toHaveLength(1);
    expect(screen.getByRole('option', { name: /Google · configured/ })).toBeTruthy();
    expect(screen.getByRole('option', { name: /Outlook\.com · not configured/ })).toBeTruthy();
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
    expect(await screen.findByLabelText('Provider')).toHaveProperty('value', 'outlook.com');
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    const summary = await screen.findByText('3 of 3 configured');
    expect(summary.closest('details')?.open).toBe(true);
    expect(screen.getByLabelText('Provider')).toHaveProperty('value', 'outlook.com');
    expect(screen.getByText('Saved.')).toBeTruthy();
  });

  it('pre-fills the client ID, never the secret, and shows the relay URI under Google', async () => {
    renderPanel();
    await choose('google');
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
    await choose('google');
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('google', { clientId: 'g-cid' });
    });
    expect(await screen.findByText('Saved.')).toBeTruthy();
  });

  it('sends the tenant for Outlook.com and shows the server reason on a 400', async () => {
    savePimProviderApp.mockRejectedValueOnce(
      new HttpError(400, 'tenantId is required for outlook.com'),
    );
    renderPanel();
    await choose('outlook.com');
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: 'ms-cid' } });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('outlook.com', {
        clientId: 'ms-cid',
        tenantId: '',
      });
    });
    expect((await screen.findByRole('alert')).textContent).toMatch(/tenantId is required/);
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
    await choose('google');
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
