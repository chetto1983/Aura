import { useTranslation } from 'react-i18next';
import { useConversation } from '../conversations/useConversations';
import { ContextBudgetGauge } from './ContextBudgetGauge';
import {
  addTurn,
  cacheHitPercent,
  DEFAULT_CONTEXT_WINDOW,
  formatCost,
  formatTokens,
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
}

export function RuntimeFooter({ usage, conversationId, windowTokens }: RuntimeFooterProps) {
  const { t } = useTranslation();
  const { data: conv } = useConversation(conversationId);

  // Session = persisted aggregate seed + the live in-flight turn (if any).
  const seed = seedSession(conv);
  const session = usage ? addTurn(seed, usage) : seed;
  const turn = usage ?? null;

  const none = t('footer.none');

  // Per-turn cache % (/0-guarded) and cost (undefined cost_usd → em-dash).
  const turnCachePct = turn ? cacheHitPercent(turn.cacheHitTokens, turn.promptTokens) : undefined;
  const turnCost = turn ? formatCost(turn.costUsd ?? 0, turn.costUsd !== undefined) : undefined;
  const sessionCachePct = cacheHitPercent(session.cacheHitTokens, session.promptTokens);
  const sessionCost = formatCost(session.costUsd, session.hasCost);

  const usedTokens = turn?.promptTokens ?? session.promptTokens;

  return (
    <footer
      aria-label={t('footer.contextLabel')}
      className="flex flex-wrap items-center gap-x-6 gap-y-2 border-t border-border bg-surface px-3 py-2 sm:px-4"
    >
      <Metric
        label={t('footer.tokens')}
        value={formatTokens(turn ? turn.promptTokens + turn.completionTokens : 0)}
        session={formatTokens(session.promptTokens + session.completionTokens)}
        sessionLabel={t('footer.session')}
      />
      <Metric
        label={t('footer.cache')}
        value={turnCachePct === undefined ? none : `${String(turnCachePct)}%`}
        session={sessionCachePct === undefined ? none : `${String(sessionCachePct)}%`}
        sessionLabel={t('footer.session')}
      />
      <Metric
        label={t('footer.cost')}
        value={turnCost ?? none}
        session={sessionCost ?? none}
        sessionLabel={t('footer.session')}
      />
      <ContextBudgetGauge
        usedTokens={usedTokens}
        windowTokens={windowTokens ?? DEFAULT_CONTEXT_WINDOW}
        conversationId={conversationId}
      />
    </footer>
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
      <span className="text-[0.625rem] font-medium uppercase tracking-wider text-text-faint">
        {label}
      </span>
      <span className="flex items-baseline gap-2">
        <span className="font-mono text-sm text-text">{value}</span>
        <span className="font-mono text-[0.6875rem] text-text-muted" title={sessionLabel}>
          {sessionLabel} {session}
        </span>
      </span>
    </div>
  );
}
