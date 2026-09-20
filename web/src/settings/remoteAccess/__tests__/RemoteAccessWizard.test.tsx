import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../../i18n/i18n';
import { RemoteAccessPanel } from '../RemoteAccessPanel';

function renderRemoteAccess() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RemoteAccessPanel />
    </QueryClientProvider>,
  );
}

describe('RemoteAccessWizard', () => {
  it('resumes at nameservers without asking for the token again', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              enabled: true,
              phase: 'waiting_nameservers',
              api_token_set: true,
              tunnel_token_set: false,
              connector: 'disconnected',
              generation: 2,
              account_id: 'account',
              zone_name: 'example.com',
              nameservers: ['ada.ns.cloudflare.com', 'bob.ns.cloudflare.com'],
            }),
          ),
        ),
      ),
    );

    renderRemoteAccess();

    expect(await screen.findByText('ada.ns.cloudflare.com')).toBeTruthy();
    expect(screen.queryByLabelText('Cloudflare API token')).toBeNull();
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(1);
    expect(screen.getByRole('list', { name: 'Remote access setup progress' })).toBeTruthy();
    expect(screen.getByRole('heading', { level: 2 }).closest('section')?.className).toContain(
      'min-w-0',
    );
  });

  it('keeps write-only candidate tokens out of the mutation cache and clears them after configure', async () => {
    let reads = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url.endsWith('/token/verify')) {
          return Promise.resolve(
            new Response(JSON.stringify({ accounts: [{ id: 'one', name: 'One' }] })),
          );
        }
        if (url === '/api/settings/remote-access' && reads++ > 0) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                enabled: true,
                phase: 'provisioning',
                api_token_set: true,
                tunnel_token_set: false,
                connector: 'disconnected',
                generation: 2,
                account_id: 'one',
                zone_name: 'example.com',
                acceptance_required: true,
              }),
            ),
          );
        }
        return Promise.resolve(
          new Response(
            JSON.stringify({
              enabled: false,
              phase: 'disabled',
              api_token_set: false,
              tunnel_token_set: false,
              connector: 'disconnected',
              generation: 1,
              acceptance_required: false,
            }),
          ),
        );
      }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <RemoteAccessPanel />
      </QueryClientProvider>,
    );
    const token = await screen.findByLabelText('Cloudflare API token');
    fireEvent.change(token, { target: { value: 'top-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Verify token' }));
    await screen.findByLabelText('Registered domain');
    expect(screen.queryByLabelText('Cloudflare API token')?.getAttribute('value') ?? '').toBe('');
    expect(JSON.stringify(client.getMutationCache().getAll())).not.toContain('top-secret');
    fireEvent.change(screen.getByLabelText('Registered domain'), {
      target: { value: 'example.com' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save and start setup' }));
    await screen.findByText('Provisioning Cloudflare resources');
    await waitFor(() => {
      expect(JSON.stringify(client.getMutationCache().getAll())).not.toContain('top-secret');
    });
  });
});
