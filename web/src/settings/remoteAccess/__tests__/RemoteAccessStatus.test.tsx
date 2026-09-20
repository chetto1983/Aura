import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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
});
