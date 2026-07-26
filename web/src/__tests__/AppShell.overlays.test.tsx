import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { AppShell } from '../AppShell';

function renderShell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppShell header-only runtime readiness', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url.includes('/api/conversations')) {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }));
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('summarizes runtime readiness in the header without a runtime rail or drawer trigger', async () => {
    const { container } = renderShell();

    expect(await screen.findByRole('status', { name: 'Runtime: Ready' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Open runtime status' })).toBeNull();
    expect(screen.queryByRole('dialog', { name: 'Display workspace' })).toBeNull();
    expect(container.querySelector('#runtime-rail')).toBeNull();
    expect(container.querySelector('.shell-runtime-rail')).toBeNull();

    const main = container.querySelector('main');
    if (!main) throw new Error('expected a <main> region');
    expect(main.className).toContain('shell-main');
    expect(main.className).toContain('grid-cols-1');
    expect(container.querySelector('#aura-chat-shell-v3')).not.toBeNull();
    expect(screen.getByRole('separator', { name: 'Resize conversation sidebar' })).toBeTruthy();
  });

  it('keeps the navigation drawer independent of the removed runtime overlay', () => {
    renderShell();

    fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }));

    expect(screen.getByRole('dialog', { name: 'Aura' })).toBeTruthy();
    expect(screen.queryByRole('dialog', { name: 'Display workspace' })).toBeNull();
  });
});
