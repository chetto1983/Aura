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

function nth<T>(items: readonly T[], index: number): T {
  const item = items[index];
  if (item === undefined) throw new Error(`no element at ${String(index)}`);
  return item;
}

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
  it('pre-fills the client ID, never the secret, and shows the relay URI under Google', async () => {
    renderPanel();
    const clientIds = await screen.findAllByLabelText(/client id/i);
    expect((clientIds[0] as HTMLInputElement).value).toBe('g-cid');
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
    await screen.findAllByLabelText(/client id/i);
    fireEvent.click(nth(screen.getAllByRole('button', { name: /save/i }), 0));
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
    const clientIds = await screen.findAllByLabelText(/client id/i);
    fireEvent.change(nth(clientIds, 2), { target: { value: 'ms-cid' } });
    fireEvent.click(nth(screen.getAllByRole('button', { name: /save/i }), 2));
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
    const clientIds = await screen.findAllByLabelText(/client id/i);
    fireEvent.change(nth(clientIds, 0), { target: { value: 'new-cid' } });
    fireEvent.change(screen.getByLabelText(/^Client secret/i), {
      target: { value: 'new-secret' },
    });
    fireEvent.click(nth(screen.getAllByRole('button', { name: /save/i }), 0));
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('google', {
        clientId: 'new-cid',
        clientSecret: 'new-secret',
      });
    });
  });
});
