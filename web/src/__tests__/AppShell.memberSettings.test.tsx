import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { AppShell } from '../AppShell';
import '../i18n/i18n';
import { fetchOnboardingStatus } from '../onboarding/onboardingApi';

// A member in the real shell and the real Settings workspace (spec A1, 2026-11-20): Settings is
// theirs to open, its rail carries only the personal sections, and no deep link reaches an admin
// pane. The member holds governance.write like every identity, but not identity.create, which is
// what makes an administrator in the cockpit.

vi.mock('../onboarding/onboardingApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../onboarding/onboardingApi')>();
  return { ...actual, fetchOnboardingStatus: vi.fn() };
});

const MEMBER_CAPABILITIES = ['agent.run', 'governance.read', 'governance.write'];

const ROUTES: Record<string, unknown> = {
  '/api/me': { identity_id: 'member', capabilities: MEMBER_CAPABILITIES },
  '/api/conversations': [],
  '/api/profile': {},
  '/api/settings/chatgpt/status': {
    connected: false,
    email: '',
    plan_enabled: false,
    status: 'disconnected',
  },
  '/api/connect/pim/accounts': { accounts: [] },
  '/api/connect/pim/providers': [],
};

function stubServer(): string[] {
  const paths: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = new URL(input instanceof Request ? input.url : String(input), 'http://cockpit');
      paths.push(url.pathname);
      const body = ROUTES[url.pathname] ?? { ok: true, ready: true, deps: {} };
      return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
    }),
  );
  return paths;
}

function renderShellAt(entry: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: '/*', element: <AppShell /> }], {
    initialEntries: [entry],
  });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

// Settings is a lazy chunk; its first import under a busy worker pool outlasts findBy's default 1 s.
const LAZY = { timeout: 8_000 };

function railLabels(rail: HTMLElement): string[] {
  return within(rail)
    .getAllByRole('button')
    .map((button) => button.textContent.trim());
}

describe('AppShell — a member in Settings', { timeout: 15_000 }, () => {
  beforeEach(() => {
    localStorage.clear();
    vi.mocked(fetchOnboardingStatus).mockReset();
    vi.mocked(fetchOnboardingStatus).mockResolvedValue({
      required: false,
      completed: true,
      skipped: false,
      routeRequired: false,
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('AMemberOpensSettingsAndSeesOnlyPersonalSections', async () => {
    const paths = stubServer();
    renderShellAt('/');
    const nav = await screen.findByRole('navigation', { name: 'Primary' });
    await waitFor(() => {
      expect(paths).toContain('/api/me');
    });

    fireEvent.click(await within(nav).findByRole('button', { name: 'Settings' }));

    const rail = await screen.findByRole('navigation', { name: 'Settings sections' }, LAZY);
    expect(railLabels(rail)).toEqual(['Your profile', 'Mail and calendar']);
    expect(within(nav).queryByRole('button', { name: 'Governance' })).toBeNull();
    expect(await screen.findByRole('heading', { name: 'Your ChatGPT account' })).toBeTruthy();
    expect(paths).not.toContain('/api/settings');
  });

  it('AMemberFollowsTheSettingsMailLink', async () => {
    stubServer();
    const router = renderShellAt('/?settings=mail');

    expect(await screen.findByRole('heading', { name: 'Mail and calendar' }, LAZY)).toBeTruthy();
    expect(
      screen.getByRole('button', { name: 'Mail and calendar' }).getAttribute('aria-current'),
    ).toBe('page');
    expect(await screen.findByLabelText('Email address')).toBeTruthy();
    await waitFor(() => {
      expect(router.state.location.search).toBe('');
    });
  });

  it('lands a member who follows a link to an admin pane on their profile', async () => {
    stubServer();
    renderShellAt('/?settings=backends');

    const rail = await screen.findByRole('navigation', { name: 'Settings sections' }, LAZY);
    expect(railLabels(rail)).toEqual(['Your profile', 'Mail and calendar']);
    expect(screen.getByRole('button', { name: 'Your profile' }).getAttribute('aria-current')).toBe(
      'page',
    );
    expect(screen.queryByRole('heading', { name: 'Sidecar and cloud backends' })).toBeNull();
  });
});
