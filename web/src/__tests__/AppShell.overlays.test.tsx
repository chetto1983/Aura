import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { AppShell } from '../AppShell';

// AppShell now mounts the RuntimeHealthPanel (polls /healthz + /readyz) AND the
// conversation sidebar/search (GET /api/conversations) + the runtime footer, so the
// shell needs a QueryClient, a Router (useParams/useNavigate), and a stubbed fetch.
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

describe('AppShell §3.1c intent-aware restore + §1.1b chat-lane floor', () => {
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

  function openNav() {
    fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }));
  }
  function openRuntime() {
    fireEvent.click(screen.getByRole('button', { name: 'Open runtime status' }));
  }
  function navDrawer() {
    return screen.queryByRole('dialog', { name: 'Navigation' });
  }
  function runtimeDrawer() {
    return screen.queryByRole('dialog', { name: 'Display workspace' });
  }

  it('opening the runtime overlay while the nav drawer is open auto-closes the nav', () => {
    renderShell();
    openNav();
    expect(navDrawer()).not.toBeNull();
    openRuntime();
    expect(runtimeDrawer()).not.toBeNull();
    // One heavy surface at a time: the nav drawer yields.
    expect(navDrawer()).toBeNull();
  });

  it('closing the runtime overlay explicitly restores the remembered nav drawer', () => {
    renderShell();
    openNav();
    openRuntime();
    expect(navDrawer()).toBeNull();
    // Explicit close via the panel close button → restore the nav.
    const runtime = runtimeDrawer();
    if (!runtime) throw new Error('expected the runtime drawer to be open');
    fireEvent.click(within(runtime).getByRole('button', { name: 'Close panel' }));
    expect(runtimeDrawer()).toBeNull();
    expect(navDrawer()).not.toBeNull();
  });

  it('the main grid uses the content-derived 3-column breakpoint (rails + --chat-lane-min)', () => {
    const { container } = renderShell();
    const main = container.querySelector('main');
    if (!main) throw new Error('expected a <main> region');
    // The 3-col grid is gated on the content-derived window-floor breakpoint, not a
    // guessed prefix. The chat lane carries an explicit ≥380px floor in the grid track.
    expect(main.className).toContain('lg:grid-cols-[15rem_minmax(var(--chat-lane-min),1fr)_19rem]');
  });
});
