import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { RunUsageEvent } from '../chat/runUsage';
import { fetchOnboardingStatus } from '../onboarding/onboardingApi';
import { AppShell } from '../AppShell';

const h = vi.hoisted(() => ({
  footerStates: [] as RunUsageEvent[],
  settle: undefined as (() => void) | undefined,
}));

vi.mock('../onboarding/onboardingApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../onboarding/onboardingApi')>();
  return { ...actual, fetchOnboardingStatus: vi.fn() };
});

vi.mock('../chat/RuntimeFooter', () => ({
  RuntimeFooter: ({ usageState }: { readonly usageState: RunUsageEvent }) => {
    h.footerStates.push(usageState);
    return (
      <output data-testid="usage-state">
        {usageState.phase}:{String(usageState.runId)}
      </output>
    );
  },
}));

vi.mock('../chat/ExternalStoreChat', () => ({
  ExternalStoreChat: ({
    threadId,
    onEnsureThread,
    onUsage,
  }: {
    readonly threadId: string;
    readonly onEnsureThread?: (initialPrompt: string) => Promise<string>;
    readonly onUsage?: (event: RunUsageEvent) => void;
  }) => (
    <div>
      <span data-testid="chat-thread">{threadId}</span>
      <button
        type="button"
        onClick={() => {
          void (async () => {
            onUsage?.({ runId: 1, phase: 'running', usage: undefined });
            await onEnsureThread?.('first prompt');
            h.settle = () => {
              onUsage?.({
                runId: 1,
                phase: 'settled',
                usage: {
                  promptTokens: 20,
                  completionTokens: 5,
                  cacheHitTokens: 4,
                  costUsd: 0.001,
                },
              });
            };
          })();
        }}
      >
        Start first run
      </button>
    </div>
  ),
}));

const created = {
  ID: '22222222-2222-2222-2222-222222222222',
  Title: '',
  TitleSet: false,
  IdentityID: 'operator',
  Status: 'active',
  Model: 'test',
  TotalInputTokens: 0,
  TotalOutputTokens: 0,
  TotalCachedTokens: 0,
  TotalCostUSD: 0,
  CreatedAt: '2026-07-16T00:00:00Z',
};

function LocationProbe() {
  return <output data-testid="location">{useLocation().pathname}</output>;
}

function renderShell() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/']}>
        <LocationProbe />
        <Routes>
          <Route path="/" element={<AppShell />} />
          <Route path="/c/:id" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppShell run usage ownership', () => {
  beforeEach(() => {
    h.footerStates.length = 0;
    h.settle = undefined;
    vi.mocked(fetchOnboardingStatus).mockResolvedValue({
      required: false,
      completed: true,
      skipped: false,
    });
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url === '/api/conversations' && init?.method === 'POST') {
          return Promise.resolve(
            new Response(JSON.stringify(created), {
              status: 201,
              headers: { 'Content-Type': 'application/json' },
            }),
          );
        }
        if (url.includes('/api/conversations') || url.includes('/api/approvals')) {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        if (url.includes('/api/me')) {
          return Promise.resolve(
            new Response(JSON.stringify({ identity_id: 'operator', capabilities: ['*'] }), {
              status: 200,
            }),
          );
        }
        return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}'));
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('preserves the first run while adopting its created thread route', async () => {
    renderShell();
    await screen.findByRole('button', { name: 'Start first run' });
    h.footerStates.length = 0;

    fireEvent.click(screen.getByRole('button', { name: 'Start first run' }));
    await waitFor(() => {
      expect(screen.getByTestId('chat-thread').textContent).toBe(created.ID);
      expect(screen.getByTestId('location').textContent).toBe(`/c/${created.ID}`);
      expect(h.settle).toBeTypeOf('function');
    });
    act(() => {
      h.settle?.();
    });
    await waitFor(() => {
      expect(screen.getByTestId('usage-state').textContent).toBe('settled:1');
    });

    const runningIndex = h.footerStates.findIndex(
      (event) => event.runId === 1 && event.phase === 'running',
    );
    expect(runningIndex).toBeGreaterThanOrEqual(0);
    const activeRunStates = h.footerStates.slice(runningIndex);
    expect(activeRunStates.some((event) => event.phase === 'idle')).toBe(false);
    expect(activeRunStates.at(-1)).toMatchObject({ runId: 1, phase: 'settled' });
  });
});
