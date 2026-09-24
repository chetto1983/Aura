import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { AppShell } from '../AppShell';

// The update center lives in the authenticated shell: its indicator in the real header, its
// banner and overlay over the whole shell, and nothing of it over first-run setup. The
// dialog's own rules are covered in src/update; the chat lane is stubbed to keep this light.

const onboarding = vi.hoisted(() => ({ required: false }));

vi.mock('../chat/ExternalStoreChat', () => ({ ExternalStoreChat: () => null }));
vi.mock('../conversations/ConversationSidebar', () => ({ ConversationSidebar: () => null }));
vi.mock('../conversations/SearchPanel', () => ({ SearchPanel: () => null }));
vi.mock('../chat/RuntimeFooter', () => ({ RuntimeFooter: () => null }));
vi.mock('../onboarding/onboardingApi', () => ({
  fetchOnboardingStatus: () => Promise.resolve({ required: onboarding.required }),
}));
vi.mock('../onboarding/FirstRunSetup', () => ({
  default: () => <div data-testid="first-run">First run</div>,
}));

const REV = 'abc1234def5678';

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

function stubShell(update: Record<string, unknown>) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const body = url === '/api/system/update' ? update : { ok: true, ready: true, deps: {} };
    return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

const applying = {
  managed: true,
  state: 'applying',
  running_rev: '7886200e5c1a2b',
  can_decide: false,
  deferred_until: null,
};

describe('AppShell system update', () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    onboarding.required = false;
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('puts the way back to a dismissed update in the header', async () => {
    window.sessionStorage.setItem('aura.update.dismissedRev', REV);
    stubShell({
      managed: true,
      state: 'pending',
      running_rev: '7886200e5c1a2b',
      available_rev: REV,
      can_decide: true,
      deferred_until: null,
    });
    const { container } = renderShell();

    const indicator = await screen.findByRole('button', { name: 'Update available' });
    const header = container.querySelector('header');
    if (header === null) throw new Error('expected the shell header');
    expect(within(header).getByRole('button', { name: 'Update available' })).toBe(indicator);

    fireEvent.click(indicator);

    expect(await screen.findByRole('dialog', { name: 'Update available' })).toBeTruthy();
  });

  it('covers the shell while the update applies', async () => {
    stubShell(applying);
    renderShell();

    expect(await screen.findByRole('alertdialog', { name: 'Aura is updating…' })).toBeTruthy();
  });

  it('shows nothing over first-run setup', async () => {
    onboarding.required = true;
    const fetchMock = stubShell(applying);
    renderShell();

    expect(await screen.findByTestId('first-run')).toBeTruthy();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });

    expect(fetchMock.mock.calls.some(([input]) => input === '/api/system/update')).toBe(true);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });
});
