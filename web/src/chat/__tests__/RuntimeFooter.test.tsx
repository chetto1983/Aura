import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { RuntimeFooter } from '../RuntimeFooter';
import {
  addTurn,
  cacheHitPercent,
  contextPercent,
  formatCost,
  formatTokens,
  gaugeTier,
  isContextNearFull,
  isNoSpendTurn,
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
  turnSettled?: boolean;
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
      {...(props.turnSettled !== undefined ? { turnSettled: props.turnSettled } : {})}
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
    expect(isContextNearFull(69)).toBe(false);
    expect(isContextNearFull(70)).toBe(true);
    expect(gaugeTier(69)).toBe('normal');
    expect(gaugeTier(70)).toBe('near');
    expect(gaugeTier(90)).toBe('critical');
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

  it('seedSession accepts snake_case aggregate fields without producing NaN', () => {
    const seed = seedSession({
      total_input_tokens: 3200,
      total_output_tokens: 740,
      total_cached_tokens: 160,
      total_cost_usd: 0.0214,
    });
    expect(seed.promptTokens).toBe(3200);
    expect(seed.completionTokens).toBe(740);
    expect(seed.cacheHitTokens).toBe(160);
    expect(seed.costUsd).toBe(0.0214);
    expect(seed.hasCost).toBe(true);
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

  it('detects local no-spend turns without fabricating usage', () => {
    expect(
      isNoSpendTurn({
        promptTokens: 0,
        completionTokens: 0,
        cacheHitTokens: 0,
      }),
    ).toBe(true);
    expect(
      isNoSpendTurn({
        promptTokens: 1,
        completionTokens: 0,
        cacheHitTokens: 0,
      }),
    ).toBe(false);
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

  it('renders snake_case aggregate seeds without NaN fallback text', async () => {
    vi.stubGlobal(
      'fetch',
      stubFetch({
        agg: {
          total_input_tokens: 3200,
          total_output_tokens: 740,
          total_cached_tokens: 160,
          total_cost_usd: 0.0214,
        },
      }),
    );
    const { container } = renderFooter({ conversationId: 'c-1' });
    await waitFor(() => {
      expect(container.textContent).not.toMatch(/NaN/);
      expect(screen.getByText(/Session 3\.9k/)).toBeTruthy();
      expect(screen.getByText(/Session 5%/)).toBeTruthy();
      // AC-7: the Session cost figure renders in BOTH the mobile compact summary
      // and the desktop full cluster, so it is intentionally duplicated.
      expect(screen.getAllByText(/Session \$0\.0214/).length).toBeGreaterThan(0);
    });
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
    expect(container.querySelector('.bg-danger')).not.toBeNull();
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

  it('uses the warning fill from 70% up to critical', () => {
    const usage: TurnUsage = {
      promptTokens: 75,
      completionTokens: 0,
      cacheHitTokens: 0,
      costUsd: 0.001,
    };
    const { container } = renderFooter({ usage, windowTokens: 100 });
    expect(container.querySelector('.bg-warning')).not.toBeNull();
    expect(container.querySelector('.bg-danger')).toBeNull();
  });

  it('renders the no-spend label for local zero-usage replies', () => {
    const usage: TurnUsage = {
      promptTokens: 0,
      completionTokens: 0,
      cacheHitTokens: 0,
    };
    renderFooter({ usage });
    expect(screen.getAllByText('Local reply - no model spend').length).toBeGreaterThan(0);
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

  // AC-6: the settled numeric cluster (TOKENS/CACHE/COST) sits in a single
  // role=status aria-live=polite aria-atomic region that announces on SETTLE.
  it('wraps the settled numeric cluster in a role=status aria-live=polite region', () => {
    const usage: TurnUsage = {
      promptTokens: 120,
      completionTokens: 80,
      cacheHitTokens: 60,
      costUsd: 0.0012,
    };
    const { container } = renderFooter({ usage, turnSettled: true });
    const status = container.querySelector('[role="status"]');
    expect(status).not.toBeNull();
    expect(status?.getAttribute('aria-live')).toBe('polite');
    expect(status?.getAttribute('aria-atomic')).toBe('true');
    // The numeric instruments live inside the status region (announced together).
    expect(status?.textContent).toMatch(/200/); // tokens 120+80
    expect(status?.textContent).toMatch(/50%/); // cache 60/120
    expect(status?.textContent).toMatch(/\$0\.0012/); // cost
  });

  // AC-6: while a turn is RUNNING (not settled) the live region must NOT announce
  // the in-flight figures — it holds the prior settled numbers.
  it('does not announce in-flight figures while a turn is running (settled-only)', () => {
    const settled: TurnUsage = {
      promptTokens: 100,
      completionTokens: 50,
      cacheHitTokens: 10,
      costUsd: 0.0005,
    };
    const { container, rerender } = renderFooter({ usage: settled, turnSettled: true });
    const statusBefore = container.querySelector('[role="status"]');
    expect(statusBefore?.textContent).toMatch(/150/); // 100+50 settled

    // A new turn streams: a different in-flight usage arrives but turnSettled=false.
    const inflight: TurnUsage = {
      promptTokens: 999,
      completionTokens: 1,
      cacheHitTokens: 0,
      costUsd: 0.5,
    };
    rerender(<RuntimeFooter conversationId="c-1" usage={inflight} turnSettled={false} />);
    const statusAfter = container.querySelector('[role="status"]');
    // The live region still holds the PRIOR settled numbers, not the streaming ones.
    expect(statusAfter?.textContent).not.toMatch(/1k/); // 999+1=1000 → "1k" must NOT leak
    expect(statusAfter?.textContent).toMatch(/150/); // prior settled value retained
  });

  // AC-7: below sm the footer exposes a tap-to-expand disclosure with a compact
  // summary; the full telemetry stays present for desktop.
  it('renders a mobile disclosure control to expand the full telemetry', () => {
    const usage: TurnUsage = {
      promptTokens: 120,
      completionTokens: 80,
      cacheHitTokens: 60,
      costUsd: 0.0012,
    };
    const { container } = renderFooter({ usage, turnSettled: true });
    // A disclosure toggle carries aria-expanded and is hidden on desktop (sm:hidden).
    const toggle = container.querySelector('[aria-expanded]');
    expect(toggle).not.toBeNull();
    expect(toggle?.getAttribute('aria-expanded')).toBe('false');
    expect(toggle?.className).toMatch(/sm:hidden/);
    expect(toggle?.className).toMatch(/min-h-\[44px\]/);
    // The full instrument cluster is visible from sm up (the disclosure is mobile-only).
    const full = container.querySelector('.hidden.sm\\:flex, .hidden.sm\\:grid');
    expect(full).not.toBeNull();
  });

  it('expands the mobile disclosure on click (aria-expanded toggles true)', () => {
    const usage: TurnUsage = {
      promptTokens: 120,
      completionTokens: 80,
      cacheHitTokens: 60,
      costUsd: 0.0012,
    };
    const { container } = renderFooter({ usage, turnSettled: true });
    const toggle = container.querySelector('[aria-expanded]');
    if (toggle === null) throw new Error('expected a mobile disclosure toggle');
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
  });
});
