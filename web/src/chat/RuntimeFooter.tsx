import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useConversation } from '../conversations/useConversations';
import { ContextBudgetGauge } from './ContextBudgetGauge';
import {
  addTurn,
  cacheHitPercent,
  contextPercent,
  DEFAULT_CONTEXT_WINDOW,
  formatCost,
  formatTokens,
  isNoSpendTurn,
  seedSession,
} from './footerMetrics';
import type { TurnUsage } from './sseAdapter';

// RuntimeFooter (CHAT-04 / D-10/D-12) is the one runtime instrument cluster
// spanning the AppShell bottom: Tokens · Cache · Cost · Context. It reuses the
// RuntimeHealthPanel mono-metric idiom (all numbers font-mono). Per-turn metrics
// come off the live SSE STATE_DELTA usage (the chat lane's onUsage seam, parsed by
// usageFromStateDelta). Session-cumulative SEEDS from the persisted conversation
// aggregates (GET /api/conversations/{id}; D-10 reload seed) then adds the live
// in-flight turn — no double-count, because the backend persists each finalized
// turn into the aggregate, so the only delta not yet counted is the current turn.
//
// Guards at the presentation boundary: cache-% /0 → "—" (never NaN%); a missing
// cost_usd → "—" (never $NaN). This is a runtime instrument, NOT a typed display —
// kept entirely off the Phase-26 typed-display namespace (no payload-type routing).

export interface RuntimeFooterProps {
  /** The latest per-turn usage off the chat lane's onUsage seam (undefined idle). */
  readonly usage: TurnUsage | undefined;
  /** The open conversation; seeds the session aggregates + the gauge marker read. */
  readonly conversationId: string;
  /** Model context window override (defaults to the DeepSeek-V4 1M window). */
  readonly windowTokens?: number;
  /**
   * Whether the latest usage is the SETTLED end-of-turn figure (true) or an
   * in-flight streaming value (false). Only settled usage feeds the aria-live
   * region so a screen reader announces once per finished turn, never per frame
   * (AC-6). AppShell wires usage on every onUpdate, so the default is `true`.
   */
  readonly turnSettled?: boolean;
}

// The settled numeric instrument strings the aria-live region announces: the
// per-turn value AND the session figure of each instrument, latched together so
// a streaming turn never makes the region chatty (AC-6).
interface NumericCluster {
  readonly tokens: string;
  readonly cache: string;
  readonly cost: string;
  readonly sessionTokens: string;
  readonly sessionCache: string;
  readonly sessionCost: string;
}

export function RuntimeFooter({
  usage,
  conversationId,
  windowTokens,
  turnSettled = true,
}: RuntimeFooterProps) {
  const { t } = useTranslation();
  const { data: conv } = useConversation(conversationId);
  const [expanded, setExpanded] = useState(false);

  // Session = persisted aggregate seed + the live in-flight turn (if any).
  const seed = seedSession(conv);
  const session = usage ? addTurn(seed, usage) : seed;
  const turn = usage ?? null;

  const none = t('footer.none');
  const noSpend = isNoSpendTurn(turn ?? undefined);
  const noSpendLabel = t('footer.noSpend');
  const sessionLabel = t('footer.session');

  // Per-turn cache % (/0-guarded) and cost (undefined cost_usd → em-dash).
  const turnCachePct = turn ? cacheHitPercent(turn.cacheHitTokens, turn.promptTokens) : undefined;
  const turnCost = turn ? formatCost(turn.costUsd ?? 0, turn.costUsd !== undefined) : undefined;
  const sessionCachePct = cacheHitPercent(session.cacheHitTokens, session.promptTokens);
  const sessionCost = formatCost(session.costUsd, session.hasCost);

  const usedTokens = turn?.promptTokens ?? session.promptTokens;

  // The live numeric cluster (visually current). It feeds the aria-live region
  // only when the turn has SETTLED — during streaming the region holds the prior
  // settled numbers (the gauge, outside the region, still updates live). AC-6.
  const liveCluster: NumericCluster = {
    tokens: noSpend
      ? noSpendLabel
      : turn
        ? formatTokens(turn.promptTokens + turn.completionTokens)
        : none,
    cache: noSpend || turnCachePct === undefined ? none : `${String(turnCachePct)}%`,
    cost: noSpend ? none : (turnCost ?? none),
    sessionTokens: formatTokens(session.promptTokens + session.completionTokens),
    sessionCache: sessionCachePct === undefined ? none : `${String(sessionCachePct)}%`,
    sessionCost: sessionCost ?? none,
  };
  const announced = useSettledCluster(liveCluster, turnSettled);

  const window = windowTokens ?? DEFAULT_CONTEXT_WINDOW;
  const ctxPct = contextPercent(usedTokens, window);

  return (
    <footer aria-label={t('footer.runtimeLabel')} className="bg-surface px-3 py-2 sm:px-4">
      {/* AC-6: the settled numeric cluster announces once per finished turn. */}
      <div
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="flex flex-wrap items-center gap-x-6 gap-y-2"
      >
        {/* AC-7 mobile compact summary + tap-to-expand disclosure (hidden ≥sm):
            Session cost + context % are the two figures an operator scans on a
            phone; the full trio expands below it. */}
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls="footer-telemetry-detail"
          onClick={() => {
            setExpanded((v) => !v);
          }}
          className="flex min-h-[1.5rem] items-baseline gap-3 py-1 font-mono text-xs text-text-muted outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent sm:hidden"
        >
          <span className="text-text">
            {sessionLabel} {announced.sessionCost}
          </span>
          <span className="text-text-faint">{`${String(ctxPct)}%`}</span>
        </button>

        {/* Full instrument cluster: always on ≥sm; on mobile only when expanded. */}
        <div
          id="footer-telemetry-detail"
          className={`${expanded ? 'flex' : 'hidden'} flex-wrap items-center gap-x-6 gap-y-2 sm:flex`}
        >
          <Metric
            label={t('footer.tokens')}
            value={announced.tokens}
            session={announced.sessionTokens}
            sessionLabel={sessionLabel}
          />
          <Metric
            label={t('footer.cache')}
            value={announced.cache}
            session={announced.sessionCache}
            sessionLabel={sessionLabel}
          />
          <Metric
            label={t('footer.cost')}
            value={announced.cost}
            session={announced.sessionCost}
            sessionLabel={sessionLabel}
          />
        </div>
      </div>

      {/* The gauge sits outside the live region: it updates the fill live every
          frame (role=progressbar carries its own semantics) without making the
          status region chatty. */}
      <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-2">
        <ContextBudgetGauge
          usedTokens={usedTokens}
          windowTokens={window}
          conversationId={conversationId}
        />
      </div>
    </footer>
  );
}

// useSettledCluster latches the last SETTLED numeric cluster: while a turn is
// streaming (settled === false) the announced values stay at the prior settled
// turn so the aria-live region announces once per finished turn, not per frame.
// It uses React's sanctioned "store info from prior renders" pattern — setState
// DURING render (never in an effect, never a ref read at render) — so the latched
// snapshot updates synchronously when a turn settles without cascading renders.
function useSettledCluster(live: NumericCluster, settled: boolean): NumericCluster {
  const [latched, setLatched] = useState<NumericCluster>(live);
  if (settled && !clusterEquals(latched, live)) {
    setLatched(live);
    return live;
  }
  return settled ? live : latched;
}

function clusterEquals(a: NumericCluster, b: NumericCluster): boolean {
  return (
    a.tokens === b.tokens &&
    a.cache === b.cache &&
    a.cost === b.cost &&
    a.sessionTokens === b.sessionTokens &&
    a.sessionCache === b.sessionCache &&
    a.sessionCost === b.sessionCost
  );
}

interface MetricProps {
  readonly label: string;
  readonly value: string;
  readonly session: string;
  readonly sessionLabel: string;
}

// A single instrument: a micro caption label over the per-turn value, with the
// session-cumulative figure beside it. All numbers are font-mono (UI-SPEC §Typography
// — Mono carries every numeric instrument).
function Metric({ label, value, session, sessionLabel }: MetricProps) {
  return (
    <div className="flex flex-col">
      <span className="text-[0.75rem] font-medium uppercase tracking-wider text-text-faint">
        {label}
      </span>
      <span className="flex items-baseline gap-2">
        <span className="font-mono text-sm text-text">{value}</span>
        <span className="font-mono text-[0.75rem] text-text-muted" title={sessionLabel}>
          {sessionLabel} {session}
        </span>
      </span>
    </div>
  );
}
