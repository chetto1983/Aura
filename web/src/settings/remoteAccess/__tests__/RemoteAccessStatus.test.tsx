import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../../i18n/i18n';
import { RemoteAccessStatus } from '../RemoteAccessStatus';
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
});
