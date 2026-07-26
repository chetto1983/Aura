import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AppShell } from '../AppShell';
import i18n from '../i18n/i18n';
import { fetchOnboardingStatus } from '../onboarding/onboardingApi';

vi.mock('../onboarding/onboardingApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../onboarding/onboardingApi')>();
  return {
    ...actual,
    fetchOnboardingStatus: vi.fn(),
  };
});

vi.mock('../onboarding/OnboardingWizard', () => ({
  default: ({ onClose }: { readonly onClose: () => void }) => (
    <div role="dialog" aria-label="Create identity">
      <button type="button" onClick={onClose}>
        Close
      </button>
    </div>
  ),
}));

vi.mock('../onboarding/FirstRunSetup', () => ({
  default: ({ onClose }: { readonly onClose: () => void }) => (
    <div role="dialog" aria-label="Set up profile">
      <button type="button" onClick={onClose}>
        Close
      </button>
    </div>
  ),
}));

vi.mock('../settings/SettingsWorkspace', () => ({
  default: ({ onCreateIdentity }: { readonly onCreateIdentity?: () => void }) => (
    <div>
      <div>Runtime settings</div>
      <button type="button" onClick={() => onCreateIdentity?.()}>
        Create identity
      </button>
    </div>
  ),
}));

vi.mock('../documents/DocumentsWorkspace', () => ({
  default: () => <div>Document library</div>,
}));

vi.mock('../graph/GraphExplorer', () => ({
  default: () => <div data-testid="graph-workspace">Evidence graph</div>,
}));

// The marketing-hero copy this operator console must NOT ship (ux-spec §350 / SC4).
const MARKETING_HERO_BLOCKLIST = [
  /get started for free/i,
  /sign up today/i,
  /trusted by/i,
  /supercharge/i,
  /unlock your/i,
  /the future of/i,
];

// AppShell mounts the header RuntimeStatusChip (polls /readyz), the conversation
// sidebar/search (GET /api/conversations), and the runtime footer, so the
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

describe('AppShell', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.mocked(fetchOnboardingStatus).mockReset();
    vi.mocked(fetchOnboardingStatus).mockResolvedValue({
      required: false,
      completed: true,
      skipped: false,
    });
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        // The signed-in operator is the seeded `local` admin ('*' wildcard), so the admin
        // surfaces (Settings/Governance) stay in the nav (MUSR-01 / D-03).
        if (url.includes('/api/me')) {
          return Promise.resolve(
            new Response(JSON.stringify({ identity_id: 'local', capabilities: ['*'] }), {
              status: 200,
            }),
          );
        }
        // The conversation list / rot-events read as a JSON array; health as an object.
        if (url.includes('/api/conversations')) {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }));
      }),
    );
  });

  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('renders the Aura brand mark', () => {
    const { container } = renderShell();
    expect(screen.getByRole('img', { name: /aura/i })).toBeTruthy();
    expect(container.firstElementChild?.className).toContain('aura-shell');
    expect(container.querySelector('.shell-header')).toBeTruthy();
    expect(container.querySelector('.shell-main')).toBeTruthy();
  });

  it('exposes an accessible conversation rail resizer on the chat surface', () => {
    renderShell();

    expect(screen.getByRole('separator', { name: 'Resize conversation sidebar' })).toBeTruthy();
  });

  it('reserves a normal-flow controls row above the chat workspace', () => {
    const { container } = renderShell();
    const controls = container.querySelector('[data-chat-workspace-controls]');
    if (!(controls instanceof HTMLElement)) throw new Error('expected chat workspace controls');

    expect(controls.classList.contains('absolute')).toBe(false);
    expect(controls.classList.contains('shrink-0')).toBe(true);
    expect(controls.parentElement?.classList.contains('grid')).toBe(true);
    expect(controls.parentElement?.classList.contains('grid-rows-[auto_minmax(0,1fr)]')).toBe(true);
    expect(controls.nextElementSibling).not.toBeNull();
  });

  it('uses New chat as the sidebar primary action instead of identity creation', () => {
    renderShell();

    expect(screen.getByRole('button', { name: 'New chat' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Create identity' })).toBeNull();
  });

  it('switches the cockpit shell to Italian', async () => {
    renderShell();
    fireEvent.click(screen.getByRole('button', { name: 'Italiano' }));

    expect(screen.getByRole('navigation', { name: 'Principale' })).toBeTruthy();
    expect(screen.getAllByText('Albero').length).toBeGreaterThan(0);
    // The left aside now hosts the conversation manager (replaced the placeholder
    // section labels); its heading is localised.
    expect(screen.getByText('Conversazioni')).toBeTruthy();
    expect(await screen.findByRole('status', { name: 'Stato runtime: Pronto' })).toBeTruthy();
  });

  it('ships no marketing-hero copy in the primary viewport', () => {
    const { container } = renderShell();
    const text = container.textContent ?? '';
    for (const pattern of MARKETING_HERO_BLOCKLIST) {
      expect(text).not.toMatch(pattern);
    }
  });

  it('mounts the runtime footer cluster (Tokens · Cache · Cost · Context)', () => {
    renderShell();
    expect(screen.getByText('Tokens')).toBeTruthy();
    expect(screen.getByText('Cache')).toBeTruthy();
    expect(screen.getByText('Cost')).toBeTruthy();
    // The Context label appears in both the footer caption and the gauge label.
    expect(screen.getAllByText('Context').length).toBeGreaterThan(0);
  });

  it('uses the header readiness status instead of a permanent runtime rail', async () => {
    const { container } = renderShell();

    expect(await screen.findByRole('status', { name: 'Runtime: Ready' })).toBeTruthy();
    expect(container.querySelector('#runtime-rail')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open runtime status' })).toBeNull();
  });

  it('opens the onboarding wizard when first-run setup is still required', async () => {
    vi.mocked(fetchOnboardingStatus).mockResolvedValueOnce({
      required: true,
      completed: false,
      skipped: false,
    });

    renderShell();

    expect(await screen.findByRole('dialog', { name: 'Set up profile' })).toBeTruthy();
    expect(fetchOnboardingStatus).toHaveBeenCalledTimes(1);
  }, 15_000);

  it('opens the runtime settings workspace from the shell mode switcher', async () => {
    renderShell();

    // Settings is admin-gated and appears once /api/me resolves the operator's '*' grant.
    fireEvent.click(
      await within(screen.getByRole('navigation', { name: 'Primary' })).findByRole('button', {
        name: 'Settings',
      }),
    );

    await waitFor(() => {
      expect(screen.getByText('Runtime settings')).toBeTruthy();
    });
  });

  it('opens the identity wizard from the settings workspace action', async () => {
    renderShell();

    // Settings is admin-gated and appears once /api/me resolves the operator's '*' grant.
    fireEvent.click(
      await within(screen.getByRole('navigation', { name: 'Primary' })).findByRole('button', {
        name: 'Settings',
      }),
    );
    await screen.findByText('Runtime settings');
    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));

    expect(await screen.findByRole('dialog', { name: 'Create identity' })).toBeTruthy();
  });

  it('hides the admin surfaces from the nav for a non-admin identity (MUSR-01 / D-03)', async () => {
    // A provisioned user holds only agent.run — not governance.write — so Settings + Governance
    // are dropped from the switcher while chat and the read surfaces remain.
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url.includes('/api/me')) {
          return Promise.resolve(
            new Response(JSON.stringify({ identity_id: 'u2', capabilities: ['agent.run'] }), {
              status: 200,
            }),
          );
        }
        if (url.includes('/api/conversations')) {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }));
      }),
    );
    renderShell();
    const nav = await screen.findByRole('navigation', { name: 'Primary' });
    await waitFor(() => {
      expect(within(nav).queryByRole('button', { name: 'Settings' })).toBeNull();
    });
    expect(within(nav).queryByRole('button', { name: 'Governance' })).toBeNull();
    expect(within(nav).getByRole('button', { name: 'Chat' })).toBeTruthy();
  });

  it('opens the document library workspace from the shell mode switcher', async () => {
    renderShell();

    fireEvent.click(
      within(screen.getByRole('navigation', { name: 'Primary' })).getByRole('button', {
        name: 'Documents',
      }),
    );

    await waitFor(() => {
      expect(screen.getByText('Document library')).toBeTruthy();
    });
  });

  it('keeps the conversation navigation scoped to the chat surface', async () => {
    const { container } = renderShell();

    expect(screen.getByText('Conversations')).toBeTruthy();

    fireEvent.click(
      within(screen.getByRole('navigation', { name: 'Primary' })).getByRole('button', {
        name: 'Graph',
      }),
    );

    await waitFor(() => {
      expect(screen.getByTestId('graph-workspace')).toBeTruthy();
    });
    expect(screen.queryByText('Conversations')).toBeNull();
    expect(screen.queryByPlaceholderText('Search conversations')).toBeNull();
    expect(screen.queryByRole('separator', { name: 'Resize conversation sidebar' })).toBeNull();
    expect(container.querySelector('.shell-side-nav')).toBeNull();
    expect(container.querySelector('main')?.className).toContain('lg:grid-cols-[minmax(0,1fr)]');
  });

  it('opens the real Aura mobile sidebar from non-chat surfaces', async () => {
    renderShell();

    fireEvent.click(
      within(screen.getByRole('navigation', { name: 'Primary' })).getByRole('button', {
        name: 'Graph',
      }),
    );

    await waitFor(() => {
      expect(screen.getByTestId('graph-workspace')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }));

    const drawer = screen.getByRole('dialog', { name: 'Aura' });
    const nav = within(drawer).getByRole('navigation', { name: 'Modes' });
    expect(within(nav).getByRole('button', { name: 'Chat' })).toBeTruthy();
    expect(within(nav).getByRole('button', { name: 'Documents' })).toBeTruthy();
    expect(within(nav).getByRole('button', { name: 'Settings' })).toBeTruthy();
    expect(within(drawer).queryByText('Projects')).toBeNull();
    expect(within(drawer).queryByText('Scheduled')).toBeNull();
  });

  it('does not fall back to the legacy passphrase logout route', async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        calls.push(`${init?.method ?? 'GET'} ${url}`);
        if (url === '/api/auth/config') {
          return Promise.resolve(new Response('auth unavailable', { status: 500 }));
        }
        if (url.includes('/api/conversations')) {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }));
      }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route path="/" element={<AppShell />} />
            <Route path="/login" element={<div>login page</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    const signOut = screen.getByRole('button', { name: 'Sign out' });
    expect(signOut.getAttribute('data-slot')).toBe('button');
    fireEvent.click(signOut);

    await waitFor(() => {
      expect(calls).toContain('GET /api/auth/config');
      expect(screen.queryByText('login page')).toBe(null);
    });
    expect(calls).not.toContain('POST /logout');
  });

  it('uses the Authula sign-out endpoint with its CSRF token when configured', async () => {
    let signOut: RequestInit | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url === '/api/auth/config') {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                provider: 'authula',
                auth_base_path: '/auth',
                csrf_header_name: 'X-AUTHULA-CSRF-TOKEN',
                csrf_token: 'csrf-token',
              }),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        if (url === '/auth/sign-out') {
          signOut = init;
          return Promise.resolve(new Response('{"message":"signed out"}', { status: 200 }));
        }
        if (url.includes('/api/conversations')) {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }));
      }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route path="/" element={<AppShell />} />
            <Route path="/login" element={<div>login page</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));

    await waitFor(() => {
      expect(signOut?.method).toBe('POST');
      expect(signOut?.credentials).toBe('same-origin');
      expect(signOut?.headers).toMatchObject({ 'X-AUTHULA-CSRF-TOKEN': 'csrf-token' });
      expect(screen.getByText('login page')).toBeTruthy();
    });
  });
});
