import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RuntimeStatusChip } from '../shell/RuntimeStatusChip';
import i18n from '../i18n/i18n';

function renderChip() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RuntimeStatusChip />
    </QueryClientProvider>,
  );
}

function stubReadyz(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      const answer = url === '/readyz' ? { status, body } : { status: 200, body: { ok: true } };
      return Promise.resolve(new Response(JSON.stringify(answer.body), { status: answer.status }));
    }),
  );
}

describe('RuntimeStatusChip', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    void i18n.changeLanguage('en');
  });

  it('opens on click and says why the runtime is degraded', async () => {
    stubReadyz(503, { status: 'unavailable', ready: false, reasons: ['memory_unavailable'] });
    renderChip();

    expect(await screen.findByRole('status', { name: 'Runtime: Degraded' })).toBeTruthy();
    expect(screen.queryByText('Memory is not answering')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Runtime: Degraded. Show details' }));

    expect(await screen.findByText('Memory is not answering')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Runtime', level: 2 })).toBeTruthy();
  });

  it('opens a ready runtime too, with no reasons to list', async () => {
    stubReadyz(200, { status: 'ready', ready: true, reasons: [] });
    renderChip();

    fireEvent.click(await screen.findByRole('button', { name: 'Runtime: Ready. Show details' }));

    expect(await screen.findByRole('heading', { name: 'Runtime', level: 2 })).toBeTruthy();
    expect(screen.queryByRole('list', { name: 'Why' })).toBeNull();
  });

  it('marks the dot by tone and stays readable when /readyz cannot be reached', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('connection refused'))),
    );
    const { container } = renderChip();

    expect(await screen.findByRole('status', { name: 'Runtime: Unavailable' })).toBeTruthy();
    expect(container.querySelector('[aria-hidden="true"]')?.className).toContain('bg-danger');
  });

  it('speaks Italian', async () => {
    await i18n.changeLanguage('it');
    stubReadyz(503, { status: 'unavailable', ready: false, reasons: ['memory_unavailable'] });
    renderChip();

    fireEvent.click(
      await screen.findByRole('button', { name: 'Stato runtime: Degradato. Mostra dettagli' }),
    );

    expect(await screen.findByText('La memoria non risponde')).toBeTruthy();
  });
});
