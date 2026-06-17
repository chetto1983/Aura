import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { RuntimeFooter } from '../RuntimeFooter';
import {
  addTurn,
  cacheHitPercent,
  contextPercent,
  formatCost,
  formatTokens,
  isContextNearFull,
  seedSession,
  totalPairsDropped,
  type ConversationAggregate,
} from '../footerMetrics';
import type { TurnUsage } from '../sseAdapter';

const AGG: ConversationAggregate = {
  TotalInputTokens: 1000,
  TotalOutputTokens: 500,
  TotalCachedTokens: 400,
  TotalCostUSD: 0.05,
};

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

// Stub GET /api/conversations/{id} (aggregate seed) + /rot-events (compaction).
function stubFetch(opts?: { agg?: ConversationAggregate; rotEvents?: { PairsDropped: number }[] }) {
  return vi.fn((input: RequestInfo | URL) => {
    const url = urlOf(input);
    if (url.includes('/rot-events')) {
      return Promise.resolve(
        new Response(
          JSON.stringify(
            (opts?.rotEvents ?? []).map((e) => ({
              TS: '2026-06-17T00:00:00Z',
              Action: 'hard_drop_pairs',
              PairsDropped: e.PairsDropped,
              TokensBefore: 100,
              TokensAfter: 60,
            })),
          ),
          { status: 200 },
        ),
      );
    }
    // The single-conversation aggregate.
    return Promise.resolve(new Response(JSON.stringify(opts?.agg ?? AGG), { status: 200 }));
  });
}

function renderFooter(props: {
  usage?: TurnUsage;
  conversationId?: string;
  windowTokens?: number;
}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <RuntimeFooter
      conversationId={props.conversationId ?? 'c-1'}
      {...(props.usage ? { usage: props.usage } : { usage: undefined })}
      {...(props.windowTokens !== undefined ? { windowTokens: props.windowTokens } : {})}
    />,
    { wrapper: Wrapper },
  );
}

describe('footerMetrics (pure)', () => {
  it('cacheHitPercent guards /0 (promptTokens=0 → undefined, never NaN)', () => {
    expect(cacheHitPercent(0, 0)).toBeUndefined();
    expect(cacheHitPercent(50, 0)).toBeUndefined();
    expect(cacheHitPercent(50, 100)).toBe(50);
  });

  it('formatCost returns undefined when no cost is known (never $NaN)', () => {
    expect(formatCost(0, false)).toBeUndefined();
    expect(formatCost(Number.NaN, true)).toBeUndefined();
    expect(formatCost(0.0012, true)).toBe('$0.0012');
    expect(formatCost(2.5, true)).toBe('$2.50');
  });

  it('formatTokens compacts thousands / millions', () => {
    expect(formatTokens(900)).toBe('900');
    expect(formatTokens(42_000)).toBe('42k');
    expect(formatTokens(1500)).toBe('1.5k');
    expect(formatTokens(120_000)).toBe('120k');
    expect(formatTokens(1_500_000)).toBe('1.5M');
  });

  it('contextPercent clamps + guards a zero window', () => {
    expect(contextPercent(50, 0)).toBe(0);
    expect(contextPercent(50, 100)).toBe(50);
    expect(contextPercent(200, 100)).toBe(100);
    expect(contextPercent(-5, 100)).toBe(0);
  });

  it('isContextNearFull flips at the ≥85% threshold', () => {
    expect(isContextNearFull(84)).toBe(false);
    expect(isContextNearFull(85)).toBe(true);
  });

  it('seedSession + addTurn accumulate without double-count', () => {
    const seed = seedSession(AGG);
    expect(seed.promptTokens).toBe(1000);
    expect(seed.hasCost).toBe(true);
    const turn: TurnUsage = {
      promptTokens: 200,
      completionTokens: 100,
      cacheHitTokens: 50,
      costUsd: 0.01,
    };
    const next = addTurn(seed, turn);
    // session = seed + this ONE turn (the aggregate already holds prior turns).
    expect(next.promptTokens).toBe(1200);
    expect(next.completionTokens).toBe(600);
    expect(next.costUsd).toBeCloseTo(0.06);
  });

  it('seedSession with no aggregate is the empty session', () => {
    const seed = seedSession(undefined);
    expect(seed.promptTokens).toBe(0);
    expect(seed.hasCost).toBe(false);
  });

  it('addTurn latches hasCost only when a cost is present', () => {
    const seed = seedSession(undefined);
    const noCost = addTurn(seed, {
      promptTokens: 10,
      completionTokens: 5,
      cacheHitTokens: 0,
    });
    expect(noCost.hasCost).toBe(false);
  });

  it('totalPairsDropped sums the compaction events', () => {
    expect(totalPairsDropped([{ PairsDropped: 2 }, { PairsDropped: 3 }])).toBe(5);
    expect(totalPairsDropped([])).toBe(0);
  });
});

describe('RuntimeFooter (CHAT-04 / D-10/D-12)', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', stubFetch());
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the per-turn tokens, cache % and cost in mono off the STATE_DELTA usage', () => {
    const usage: TurnUsage = {
      promptTokens: 120,
      completionTokens: 80,
      cacheHitTokens: 60,
      costUsd: 0.0012,
    };
    const { container } = renderFooter({ usage });
    // Per-turn tokens = prompt + completion = 200.
    expect(screen.getByText('200')).toBeTruthy();
    // Cache-hit % = 60/120 = 50%.
    expect(screen.getByText('50%')).toBeTruthy();
    // Cost rendered (sub-cent precision), never $NaN.
    expect(screen.getByText('$0.0012')).toBeTruthy();
    // The numeric instruments use font-mono.
    expect(container.querySelectorAll('.font-mono').length).toBeGreaterThan(0);
  });

  it('guards the cache-% /0: promptTokens=0 renders the em-dash, never NaN', () => {
    const usage: TurnUsage = {
      promptTokens: 0,
      completionTokens: 0,
      cacheHitTokens: 0,
      costUsd: 0.001,
    };
    const { container } = renderFooter({ usage });
    expect(container.textContent).not.toMatch(/NaN/);
    // The em-dash placeholder appears for the undefined cache %.
    expect(screen.getAllByText('—').length).toBeGreaterThan(0);
  });

  it('renders the em-dash (not $NaN) when cost_usd is absent', () => {
    const usage: TurnUsage = {
      promptTokens: 100,
      completionTokens: 50,
      cacheHitTokens: 10,
      // costUsd intentionally omitted (provider did not return it).
    };
    const { container } = renderFooter({ usage });
    expect(container.textContent).not.toMatch(/\$NaN/);
    expect(container.textContent).not.toMatch(/NaN/);
  });

  it('seeds the session-cumulative from the conversation aggregate then adds the live delta', async () => {
    const usage: TurnUsage = {
      promptTokens: 200,
      completionTokens: 100,
      cacheHitTokens: 50,
      costUsd: 0.01,
    };
    renderFooter({ usage });
    await waitFor(() => {
      // session tokens = (1000+500 seed) + (200+100 live) = 1.8k.
      expect(screen.getByText(/Session 1\.8k/)).toBeTruthy();
    });
  });

  it('shows the context gauge value and switches the fill to warning at ≥85%', () => {
    const usage: TurnUsage = {
      promptTokens: 90,
      completionTokens: 0,
      cacheHitTokens: 0,
      costUsd: 0.001,
    };
    // window=100 → 90/100 = 90% (near-full).
    const { container } = renderFooter({ usage, windowTokens: 100 });
    expect(screen.getByText(/90 \/ 100 · 90%/)).toBeTruthy();
    const bar = container.querySelector('[role="progressbar"]');
    expect(bar?.getAttribute('aria-valuenow')).toBe('90');
    // The fill is the warning tone at ≥85%.
    expect(container.querySelector('.bg-warning')).not.toBeNull();
    expect(container.querySelector('.bg-accent')).toBeNull();
  });

  it('uses the accent fill below the near-full threshold', () => {
    const usage: TurnUsage = {
      promptTokens: 10,
      completionTokens: 0,
      cacheHitTokens: 0,
      costUsd: 0.001,
    };
    const { container } = renderFooter({ usage, windowTokens: 100 });
    expect(container.querySelector('.bg-accent')).not.toBeNull();
    expect(container.querySelector('.bg-warning')).toBeNull();
  });

  it('renders the microcompact marker from rot-events (pairs_dropped)', async () => {
    vi.stubGlobal('fetch', stubFetch({ rotEvents: [{ PairsDropped: 2 }, { PairsDropped: 1 }] }));
    renderFooter({ conversationId: 'c-1' });
    await waitFor(() => {
      // 2 + 1 = 3 older turns compacted.
      expect(screen.getByText('Compacted 3 older turns')).toBeTruthy();
    });
  });

  it('shows no marker when there are no compaction events', async () => {
    vi.stubGlobal('fetch', stubFetch({ rotEvents: [] }));
    renderFooter({ conversationId: 'c-1' });
    await waitFor(() => {
      expect(screen.queryByText(/Compacted/)).toBeNull();
    });
  });
});
