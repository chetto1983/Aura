import type { JSONPatchOp } from './sseAdapter_frames';

// sseAdapter_usage — the D-10 usage/cost projection helpers split out of
// sseAdapter.ts (600-LOC cap, refactor-on-touch when the id-aware SSE parsing
// landed for fix-plan 1.3B). Pure functions over STATE_DELTA JSONPatch ops;
// depends only on the leaf sseAdapter_frames module. Re-exported through the
// sseAdapter barrel so the public import surface is unchanged.

// ---------------------------------------------------------------------------
// Usage — read off the final STATE_DELTA (cost/cache footer, D-10).
// ---------------------------------------------------------------------------

export interface TurnUsage {
  readonly promptTokens: number;
  readonly completionTokens: number;
  readonly cacheHitTokens: number;
  /**
   * How much of the context window the request actually occupied: the prompt of the
   * round's FINAL call. promptTokens is the round's BILL — every call in the round added
   * together, because each re-sends the prefix — so on a tool-calling round it is several
   * requests summed and reads as a window that is full when it is not.
   */
  readonly contextTokens: number;
  readonly costUsd?: number;
}

/** True when this STATE_DELTA carries usage keys (vs the tool-result marker). */
export function isUsageDelta(ops: readonly JSONPatchOp[]): boolean {
  return ops.some(
    (o) =>
      o.path === '/prompt_tokens' ||
      o.path === '/completion_tokens' ||
      o.path === '/cache_hit_tokens' ||
      o.path === '/cost_usd',
  );
}

/** The tool_call_id a STATE_DELTA marks (Pitfall 2 — tool-result, never prose). */
export function toolCallIdFromDelta(ops: readonly JSONPatchOp[]): string | undefined {
  const marker = ops.find((o) => o.path === '/tool_call_id');
  if (marker === undefined) return undefined;
  return typeof marker.value === 'string' ? marker.value : undefined;
}

/**
 * Project the usage JSONPatch ops onto a TurnUsage. Missing cost_usd → costUsd
 * undefined (D-10: provider may omit cost). Numeric coercion is defensive.
 */
export function usageFromStateDelta(ops: readonly JSONPatchOp[]): TurnUsage {
  const byPath = new Map(ops.map((o) => [o.path, o.value]));
  const cost = byPath.get('/cost_usd');
  return {
    promptTokens: Number(byPath.get('/prompt_tokens') ?? 0),
    completionTokens: Number(byPath.get('/completion_tokens') ?? 0),
    cacheHitTokens: Number(byPath.get('/cache_hit_tokens') ?? 0),
    // Older daemons send no context_tokens; fall back to the summed prompt, which is what
    // the gauge showed before this existed.
    contextTokens: Number(byPath.get('/context_tokens') ?? byPath.get('/prompt_tokens') ?? 0),
    ...(cost !== undefined ? { costUsd: Number(cost) } : {}),
  };
}

/**
 * Cache-hit ratio (0..1) for the footer. Guards divide-by-zero: promptTokens=0
 * returns 0, never NaN (matching cachemetrics.Aggregate "ratio left to caller").
 */
export function cacheHitRatio(usage: TurnUsage): number {
  if (usage.promptTokens <= 0) return 0;
  return usage.cacheHitTokens / usage.promptTokens;
}
