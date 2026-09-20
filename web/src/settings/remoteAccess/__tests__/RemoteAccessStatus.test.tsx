import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../../i18n/i18n';
import { RemoteAccessStatus } from '../RemoteAccessStatus';
import { RemoteAccessPanel } from '../RemoteAccessPanel';
import type { RemoteAccessStatusDTO } from '../remoteAccessApi';

const status: RemoteAccessStatusDTO = {
  enabled: true,
  phase: 'healthy',
  api_token_set: true,
  tunnel_token_set: true,
  connector: 'healthy',
  generation: 3,
  public_hostname: 'aura.example.com',
  warp_hostname: 'warp.example.com',
  acceptance_required: false,
};

describe('RemoteAccessStatus', () => {
  it('requires the exact hostname before deletion and returns focus to its trigger', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const remove = vi.fn(() => Promise.resolve());
    render(
      <QueryClientProvider client={client}>
        <RemoteAccessStatus status={status} onAction={vi.fn()} onDelete={remove} />
      </QueryClientProvider>,
    );

    const trigger = screen.getByRole('button', { name: 'Delete remote access' });
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(1);
    expect(screen.getAllByRole('status')).not.toHaveLength(0);
    expect(trigger.className).toContain('min-h-[44px]');
    fireEvent.click(trigger);
    const confirm = screen.getByRole('button', { name: 'Delete permanently' });
    expect(confirm.getAttribute('disabled')).not.toBeNull();
    fireEvent.change(screen.getByLabelText('Type aura.example.com to confirm'), {
      target: { value: 'aura.example.com' },
    });
    fireEvent.click(confirm);
    expect(remove).toHaveBeenCalledWith('aura.example.com');
    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
  });

  it('keeps management controls available for disabled configured state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ ...status, enabled: false, phase: 'disabled' })),
        ),
      ),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <RemoteAccessPanel />
      </QueryClientProvider>,
    );
    expect(await screen.findByRole('button', { name: 'Delete remote access' })).toBeTruthy();
    expect(screen.getByLabelText('Cloudflare API token')).toBeTruthy();
  });

  it('replaces credentials with the current enabled state and configured labels', async () => {
    const requests: unknown[] = [];
    const disabled = {
      ...status,
      enabled: false,
      phase: 'disabled',
      account_id: 'account',
      zone_name: 'example.com',
      public_hostname: 'custom.example.com',
      warp_hostname: 'private.example.com',
    };
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url.endsWith('/token/verify'))
          return Promise.resolve(
            new Response(JSON.stringify({ accounts: [{ id: 'account', name: 'Account' }] })),
          );
        if (init?.method === 'PUT') requests.push(JSON.parse(String(init.body)));
        return Promise.resolve(new Response(JSON.stringify(disabled)));
      }),
    );
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <RemoteAccessPanel />
      </QueryClientProvider>,
    );
    fireEvent.change(await screen.findByLabelText('Cloudflare API token'), {
      target: { value: 'replacement' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Verify token' }));
    await waitFor(() =>
      expect(requests).toEqual([
        expect.objectContaining({ enabled: false, public_label: 'custom', warp_label: 'private' }),
      ]),
    );
  });

  it('keeps enabled true when replacing a healthy configured token', async () => {
    const requests: unknown[] = [];
    const healthy = { ...status, account_id: 'account', zone_name: 'example.com' };
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url.endsWith('/token/verify'))
          return Promise.resolve(
            new Response(JSON.stringify({ accounts: [{ id: 'account', name: 'Account' }] })),
          );
        if (init?.method === 'PUT') requests.push(JSON.parse(String(init.body)));
        return Promise.resolve(new Response(JSON.stringify(healthy)));
      }),
    );
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <RemoteAccessPanel />
      </QueryClientProvider>,
    );
    fireEvent.change(await screen.findByLabelText('Cloudflare API token'), {
      target: { value: 'replacement' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Verify token' }));
    await waitFor(() => expect(requests).toEqual([expect.objectContaining({ enabled: true })]));
  });

  it('contains a rejected management action in localized alert feedback', async () => {
    render(
      <RemoteAccessStatus
        status={status}
        onAction={() => Promise.reject(new Error('top-secret'))}
        onDelete={() => Promise.resolve()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Retry reconciliation' }));
    expect((await screen.findByRole('alert')).textContent).toContain('remote access action');
  });

  it('keeps a rejected delete error inside its open confirmation dialog', async () => {
    render(
      <RemoteAccessStatus
        status={status}
        onAction={() => Promise.resolve()}
        onDelete={() => Promise.reject(new Error('secret'))}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete remote access' }));
    fireEvent.change(screen.getByLabelText('Type aura.example.com to confirm'), {
      target: { value: 'aura.example.com' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Delete permanently' }));
    const dialog = screen.getByRole('alertdialog');
    expect(await within(dialog).findByRole('alert')).toBeTruthy();
    expect(dialog.textContent).toContain('remote access action');
  });
});
