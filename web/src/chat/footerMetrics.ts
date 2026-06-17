import type { TurnUsage } from './sseAdapter';

// footerMetrics holds the pure projection helpers for the runtime footer (D-10/
// D-12) so RuntimeFooter.tsx / ContextBudgetGauge.tsx export only components
// (react-refresh/only-export-components). All numbers are presentation-boundary
// formatted with /0 + undefined guards so the footer NEVER renders NaN / $NaN.

/** Session-cumulative totals: the conversation aggregates plus live turn deltas. */
export interface SessionTotals {
  readonly promptTokens: number;
  readonly completionTokens: number;
  readonly cacheHitTokens: number;
  readonly costUsd: number;
  /** True once at least one live turn (or a seeded cost) contributes a cost figure. */
  readonly hasCost: boolean;
}

/** The reload seed off GET /api/conversations/{id} aggregates (Go field names). */
export interface ConversationAggregate {
  readonly TotalInputTokens: number;
  readonly TotalOutputTokens: number;
  readonly TotalCachedTokens: number;
  readonly TotalCostUSD: number;
}

export const EMPTY_SESSION: SessionTotals = {
  promptTokens: 0,
  completionTokens: 0,
  cacheHitTokens: 0,
  costUsd: 0,
  hasCost: false,
};

/**
 * Seed the session totals from the persisted conversation aggregates (the D-10
 * reload seed). hasCost reflects whether the stored cost is meaningful (> 0) so a
 * brand-new conversation with no spend shows "—" not "$0.00".
 */
export function seedSession(agg: ConversationAggregate | undefined): SessionTotals {
  if (!agg) return EMPTY_SESSION;
  return {
    promptTokens: agg.TotalInputTokens,
    completionTokens: agg.TotalOutputTokens,
    cacheHitTokens: agg.TotalCachedTokens,
    costUsd: agg.TotalCostUSD,
    hasCost: agg.TotalCostUSD > 0,
  };
}

/**
 * Add ONE live turn's usage to the running session totals (no double-count: the
 * caller adds each distinct turn exactly once). cost only accrues when the turn
 * carried a cost_usd (provider may omit it); hasCost latches true once any turn
 * or the seed contributes a cost.
 */
export function addTurn(base: SessionTotals, turn: TurnUsage): SessionTotals {
  const turnCost = turn.costUsd ?? 0;
  return {
    promptTokens: base.promptTokens + turn.promptTokens,
    completionTokens: base.completionTokens + turn.completionTokens,
    cacheHitTokens: base.cacheHitTokens + turn.cacheHitTokens,
    costUsd: base.costUsd + turnCost,
    hasCost: base.hasCost || turn.costUsd !== undefined,
  };
}

/**
 * Cache-hit percentage (0..100, rounded) with a /0 guard: promptTokens <= 0
 * returns undefined so the caller can render "—" instead of NaN%.
 */
export function cacheHitPercent(cacheHitTokens: number, promptTokens: number): number | undefined {
  if (promptTokens <= 0) return undefined;
  return Math.round((cacheHitTokens / promptTokens) * 100);
}

/** Compact token count: 42_000 → "42k", 1_500_000 → "1.5M", < 1000 verbatim. */
export function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) {
    const k = n / 1000;
    return `${k >= 100 ? String(Math.round(k)) : trim1(k)}k`;
  }
  return `${trim1(n / 1_000_000)}M`;
}

function trim1(n: number): string {
  // One decimal, dropping a trailing ".0" (42.0k → 42k).
  return n
    .toFixed(1)
    .replace(/\.0$/, '');
}

/**
 * Format the estimated cost. Returns undefined when no cost is known (provider
 * omitted it) so the caller renders the em-dash rather than "$NaN" / "$0.00".
 */
export function formatCost(costUsd: number, hasCost: boolean): string | undefined {
  if (!hasCost || !Number.isFinite(costUsd)) return undefined;
  // Sub-cent precision matters for per-turn costs (e.g. $0.0012).
  return costUsd < 1 ? `$${costUsd.toFixed(4)}` : `$${costUsd.toFixed(2)}`;
}

/** Fill percent (0..100, clamped) of used tokens vs the model window, /0-guarded. */
export function contextPercent(used: number, window: number): number {
  if (window <= 0) return 0;
  const pct = Math.round((used / window) * 100);
  return Math.min(100, Math.max(0, pct));
}

/** ≥85% of the window is the near-full threshold → warning fill (UI-SPEC §Color). */
export const CONTEXT_NEAR_FULL_PERCENT = 85;

export function isContextNearFull(percent: number): boolean {
  return percent >= CONTEXT_NEAR_FULL_PERCENT;
}

/** Total pairs dropped by the microcompact ladder (sum of pairs_dropped events). */
export function totalPairsDropped(events: readonly { readonly PairsDropped: number }[]): number {
  return events.reduce((sum, e) => sum + e.PairsDropped, 0);
}

// The DeepSeek-V4 default context window (matches internal/llm defaultContextWindow
// = 1_000_000). The window is runtime config, not on the conversation wire shape,
// so the footer carries the default; a future plan can thread the live window in.
export const DEFAULT_CONTEXT_WINDOW = 1_000_000;
