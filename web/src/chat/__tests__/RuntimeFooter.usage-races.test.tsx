import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { CONVERSATION_KEY } from '../../conversations/useConversations';
import { RuntimeFooter } from '../RuntimeFooter';
import type { SessionTotals } from '../footerMetrics';
import type { RunSessionBaselineEvent } from '../runSessionUsage';
import type { RunUsageEvent } from '../runUsage';
import type { TurnUsage } from '../sseAdapter';

const PRE_TURN_AGGREGATE = {
  TotalInputTokens: 1000,
  TotalOutputTokens: 500,
  TotalCachedTokens: 400,
  TotalCostUSD: 0.05,
};

const POST_TURN_AGGREGATE = {
  TotalInputTokens: 1200,
  TotalOutputTokens: 600,
  TotalCachedTokens: 450,
  TotalCostUSD: 0.06,
};

const SETTLED_TURN: TurnUsage = {
  promptTokens: 200,
  contextTokens: 200,
  completionTokens: 100,
  cacheHitTokens: 50,
  costUsd: 0.01,
};

const NEXT_TURN: TurnUsage = {
  promptTokens: 100,
  contextTokens: 100,
  completionTokens: 0,
  cacheHitTokens: 0,
  costUsd: 0.002,
};

function event(runId: number, phase: RunUsageEvent['phase'], usage?: TurnUsage): RunUsageEvent {
  return { runId, phase, usage };
}

function renderFooter(usageState: RunUsageEvent, conversationId = 'c-1') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData([CONVERSATION_KEY, conversationId], PRE_TURN_AGGREGATE);
  const Wrapper = ({ children }: { readonly children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = render(<RuntimeFooter usageState={usageState} conversationId={conversationId} />, {
    wrapper: Wrapper,
  });
  return { ...view, client };
}

function renderUnseededFooter(
  usageState: RunUsageEvent,
  sessionBaseline: RunSessionBaselineEvent,
  conversationId = 'c-1',
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Promise<Response>(() => undefined)),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { readonly children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = render(
    <RuntimeFooter
      usageState={usageState}
      conversationId={conversationId}
      sessionBaseline={sessionBaseline}
    />,
    { wrapper: Wrapper },
  );
  return { ...view, client };
}

function visibleMetrics(): string {
  return screen.getByTestId('footer-visible-metrics').textContent ?? '';
}

describe('RuntimeFooter run/session ownership', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('accepts a first authoritative aggregate that already includes a fast settled turn', () => {
    const usageState = event(1, 'settled', SETTLED_TURN);
    const sessionBaseline = { runId: 1, baseline: null };
    const { client, rerender } = renderUnseededFooter(usageState, sessionBaseline);
    expect(visibleMetrics()).toContain('Session 300');

    act(() => {
      client.setQueryData([CONVERSATION_KEY, 'c-1'], {
        TotalInputTokens: 200,
        TotalOutputTokens: 100,
        TotalCachedTokens: 50,
        TotalCostUSD: 0.01,
      });
    });
    rerender(
      <RuntimeFooter
        usageState={usageState}
        conversationId="c-1"
        sessionBaseline={sessionBaseline}
      />,
    );

    expect(visibleMetrics()).toContain('Session 300');
    expect(visibleMetrics()).not.toContain('Session 600');
  });

  it('retains a locally committed turn when a rapid next run receives a late pre-turn aggregate', () => {
    const preTurnBaseline: SessionTotals = {
      promptTokens: 1000,
      completionTokens: 500,
      cacheHitTokens: 400,
      costUsd: 0.05,
      hasCost: true,
    };
    const { client, rerender } = renderUnseededFooter(event(1, 'settled', SETTLED_TURN), {
      runId: 1,
      baseline: preTurnBaseline,
    });
    const nextUsage: TurnUsage = {
      promptTokens: 100,
      contextTokens: 100,
      completionTokens: 0,
      cacheHitTokens: 0,
      costUsd: 0.002,
    };
    const rapidNext = event(2, 'running', nextUsage);
    const rapidBaseline = { runId: 2, baseline: preTurnBaseline };
    rerender(
      <RuntimeFooter usageState={rapidNext} conversationId="c-1" sessionBaseline={rapidBaseline} />,
    );

    act(() => {
      client.setQueryData([CONVERSATION_KEY, 'c-1'], PRE_TURN_AGGREGATE);
    });
    rerender(
      <RuntimeFooter usageState={rapidNext} conversationId="c-1" sessionBaseline={rapidBaseline} />,
    );

    expect(visibleMetrics()).toContain('Session 1.9k');
    expect(visibleMetrics()).not.toContain('Session 1.6k');
  });

  it('preserves locally committed totals across consecutive unavailable baselines', () => {
    const { rerender } = renderUnseededFooter(event(1, 'settled', SETTLED_TURN), {
      runId: 1,
      baseline: null,
    });
    expect(visibleMetrics()).toContain('Session 300');

    rerender(
      <RuntimeFooter
        usageState={event(2, 'running')}
        conversationId="c-1"
        sessionBaseline={{ runId: 2, baseline: null }}
      />,
    );
    expect(visibleMetrics()).toContain('Session 300');

    rerender(
      <RuntimeFooter
        usageState={event(2, 'running', NEXT_TURN)}
        conversationId="c-1"
        sessionBaseline={{ runId: 2, baseline: null }}
      />,
    );
    expect(visibleMetrics()).toContain('Session 400');

    rerender(
      <RuntimeFooter
        usageState={event(2, 'settled', NEXT_TURN)}
        conversationId="c-1"
        sessionBaseline={{ runId: 2, baseline: null }}
      />,
    );
    expect(visibleMetrics()).toContain('Session 400');
  });

  it('reconciles a late authoritative seed without dropping a null-baseline turn', () => {
    const secondRun = event(2, 'settled', NEXT_TURN);
    const secondBaseline = { runId: 2, baseline: null };
    const { client, rerender } = renderUnseededFooter(event(1, 'settled', SETTLED_TURN), {
      runId: 1,
      baseline: null,
    });
    rerender(
      <RuntimeFooter
        usageState={secondRun}
        conversationId="c-1"
        sessionBaseline={secondBaseline}
      />,
    );
    expect(visibleMetrics()).toContain('Session 400');

    act(() => {
      client.setQueryData([CONVERSATION_KEY, 'c-1'], {
        TotalInputTokens: 200,
        TotalOutputTokens: 100,
        TotalCachedTokens: 50,
        TotalCostUSD: 0.01,
      });
    });
    rerender(
      <RuntimeFooter
        usageState={secondRun}
        conversationId="c-1"
        sessionBaseline={secondBaseline}
      />,
    );

    expect(visibleMetrics()).toContain('Session 400');
    expect(visibleMetrics()).not.toContain('Session 300');
    expect(visibleMetrics()).not.toContain('Session 700');
  });

  it('does not count a settled turn again when the conversation aggregate refetch includes it', async () => {
    const { client, rerender } = renderFooter(event(1, 'settled', SETTLED_TURN));
    expect(visibleMetrics()).toContain('Session 1.8k');

    act(() => {
      client.setQueryData([CONVERSATION_KEY, 'c-1'], POST_TURN_AGGREGATE);
    });
    rerender(<RuntimeFooter usageState={event(1, 'settled', SETTLED_TURN)} conversationId="c-1" />);

    await waitFor(() => {
      expect(visibleMetrics()).toContain('Session 1.8k');
      expect(visibleMetrics()).not.toContain('Session 2.1k');
    });
  });

  it('keeps the prior settled total while a rapid next run starts against a stale aggregate', () => {
    const { rerender } = renderFooter(event(1, 'settled', SETTLED_TURN));
    expect(visibleMetrics()).toContain('Session 1.8k');

    rerender(<RuntimeFooter usageState={event(2, 'running')} conversationId="c-1" />);
    expect(visibleMetrics()).toContain('Session 1.8k');

    rerender(
      <RuntimeFooter
        usageState={event(2, 'running', {
          promptTokens: 100,
          contextTokens: 100,
          completionTokens: 0,
          cacheHitTokens: 0,
          costUsd: 0.002,
        })}
        conversationId="c-1"
      />,
    );
    expect(visibleMetrics()).toContain('Session 1.9k');
  });

  it('gives each footer instance a distinct disclosure relationship', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <RuntimeFooter usageState={event(0, 'idle')} conversationId="c-1" />
        <RuntimeFooter usageState={event(0, 'idle')} conversationId="c-2" />
      </QueryClientProvider>,
    );

    const toggles = screen.getAllByRole('button', { name: 'Show telemetry details' });
    const controls = toggles.map((toggle) => toggle.getAttribute('aria-controls'));
    expect(new Set(controls).size).toBe(2);
    for (const id of controls) {
      expect(id).not.toBeNull();
      expect(document.getElementById(id ?? '')).not.toBeNull();
    }
  });
});
