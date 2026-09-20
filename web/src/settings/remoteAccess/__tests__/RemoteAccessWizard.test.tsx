import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
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
});
