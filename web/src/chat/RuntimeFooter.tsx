import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useConversation } from '../conversations/useConversations';
import { ContextBudgetGauge } from './ContextBudgetGauge';
import {
  cacheHitPercent,
  contextPercent,
  DEFAULT_CONTEXT_WINDOW,
  formatCost,
  formatTokens,
  isNoSpendTurn,
  seedSession,
} from './footerMetrics';
import type { RunUsageEvent } from './runUsage';
import { useRunSessionUsage, type RunSessionBaselineEvent } from './runSessionUsage';
import { Button } from '@/components/ui/button';

export interface RuntimeFooterProps {
  readonly usageState: RunUsageEvent;
  readonly conversationId: string;
  readonly windowTokens?: number;
  readonly sessionBaseline?: RunSessionBaselineEvent;
}

interface NumericCluster {
  readonly tokens: string;
  readonly cache: string;
  readonly cost: string;
  readonly sessionTokens: string;
  readonly sessionCache: string;
  readonly sessionCost: string;
}

export function RuntimeFooter({
  usageState,
  conversationId,
  windowTokens,
  sessionBaseline,
}: RuntimeFooterProps) {
  const { t } = useTranslation();
  const { data: conv } = useConversation(conversationId);
  const [expanded, setExpanded] = useState(false);
  const detailId = `footer-telemetry-detail-${useId()}`;
  const turn = usageState.usage ?? null;
  const seed = seedSession(conv);
  const session = useRunSessionUsage(
    conversationId,
    seed,
    conv !== undefined,
    usageState,
    sessionBaseline,
  );
  const none = t('footer.none');
  const noSpend = isNoSpendTurn(turn ?? undefined);
  const noSpendLabel = t('footer.noSpend');
  const sessionLabel = t('footer.session');
  const turnCachePct = turn ? cacheHitPercent(turn.cacheHitTokens, turn.promptTokens) : undefined;
  const turnCost = turn ? formatCost(turn.costUsd ?? 0, turn.costUsd !== undefined) : undefined;
  const sessionCachePct = cacheHitPercent(session.cacheHitTokens, session.promptTokens);
  const sessionCost = formatCost(session.costUsd, session.hasCost);

  const liveCluster = useMemo<NumericCluster>(
    () => ({
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
    }),
    [
      noSpend,
      noSpendLabel,
      none,
      session.completionTokens,
      session.promptTokens,
      sessionCachePct,
      sessionCost,
      turn,
      turnCachePct,
      turnCost,
    ],
  );
  const settled = useSettledAnnouncement(liveCluster, usageState, conversationId);
  // The gauge measures the CURRENT request's context fill, so it rides the live turn's
  // context_tokens: the prompt of the round's FINAL call. NOT its prompt_tokens, which
  // sums every call in the round (the provider bills each one because each re-sends the
  // prefix) — measured on the live deployment 2026-08-16, a two-tool round read 72,224
  // against an 81,920 window (88%) when the largest single request was about 24k (29%),
  // then "dropped" to 17% on the next prose round. The operator saw a window filling and
  // emptying; it was the bill being added up.
  //
  // On a cold reload / idle there is no live turn and the fallback is the conversation's
  // LastInputTokens, which is still that summed figure — an UPPER BOUND on the fill until
  // the first turn of the session lands. Never session.promptTokens, which is cumulative
  // across every turn and dwarfs the window on a long chat (BUG-8).
  // Switching chat is already covered upstream: AppShell calls resetUsage() when the route
  // id changes, so `turn` never carries the previous conversation's figure into this one.
  // The DENOMINATOR is deployment-wide on purpose — the runtime replays every conversation
  // under the currently configured window, whatever model the chat was started with.
  const usedTokens = turn?.contextTokens ?? conv?.LastInputTokens ?? 0;
  const window = windowTokens ?? DEFAULT_CONTEXT_WINDOW;
  const ctxPct = contextPercent(usedTokens, window);

  return (
    <footer aria-label={t('footer.runtimeLabel')} className="bg-surface px-3 py-2 sm:px-4">
      <div
        data-testid="footer-visible-metrics"
        className="flex flex-wrap items-center gap-x-6 gap-y-2"
      >
        <Button
          type="button"
          variant="ghost"
          aria-label={t(expanded ? 'footer.hideDetails' : 'footer.showDetails')}
          aria-expanded={expanded}
          aria-controls={detailId}
          onClick={() => {
            setExpanded((value) => !value);
          }}
          className="h-auto min-h-[44px] gap-3 px-0 py-2 font-mono text-xs text-text-muted hover:bg-transparent sm:hidden"
        >
          <span className="text-text">
            {t('footer.cost')} {liveCluster.sessionCost}
          </span>
          <span className="text-text-faint">
            {t('footer.context')} {String(ctxPct)}%
          </span>
          <ChevronDown
            aria-hidden="true"
            data-testid="footer-disclosure-cue"
            className={`size-4 transition-transform motion-reduce:transition-none ${expanded ? 'rotate-180' : ''}`}
          />
        </Button>

        <div
          id={detailId}
          className={`${expanded ? 'flex' : 'hidden'} flex-wrap items-center gap-x-6 gap-y-2 sm:flex`}
        >
          <Metric
            label={t('footer.tokens')}
            value={liveCluster.tokens}
            session={liveCluster.sessionTokens}
            sessionLabel={sessionLabel}
          />
          <Metric
            label={t('footer.cache')}
            value={liveCluster.cache}
            session={liveCluster.sessionCache}
            sessionLabel={sessionLabel}
          />
          <Metric
            label={t('footer.cost')}
            value={liveCluster.cost}
            session={liveCluster.sessionCost}
            sessionLabel={sessionLabel}
          />
          <ContextBudgetGauge
            usedTokens={usedTokens}
            windowTokens={window}
            conversationId={conversationId}
          />
        </div>
      </div>
      <div
        data-testid="footer-settled-status"
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="sr-only"
      >
        {settled === null ? '' : t('footer.settledAnnouncement', { ...settled })}
      </div>
    </footer>
  );
}

function useSettledAnnouncement(
  live: NumericCluster,
  event: RunUsageEvent,
  conversationId: string,
): NumericCluster | null {
  const [value, setValue] = useState<{
    readonly conversationId: string;
    readonly cluster: NumericCluster;
  } | null>(null);
  const announcedRun = useRef<{ readonly conversationId: string; readonly runId: number } | null>(
    null,
  );
  const activeConversation = useRef(conversationId);
  const suppressedSettledRun = useRef<number | null>(null);
  useEffect(() => {
    if (activeConversation.current === conversationId) return;
    activeConversation.current = conversationId;
    announcedRun.current = null;
    suppressedSettledRun.current = event.phase === 'settled' ? event.runId : null;
  }, [conversationId, event.phase, event.runId]);
  useEffect(() => {
    if (event.phase !== 'settled') {
      suppressedSettledRun.current = null;
      return;
    }
    if (
      suppressedSettledRun.current === event.runId ||
      (announcedRun.current?.conversationId === conversationId &&
        announcedRun.current.runId === event.runId)
    )
      return;
    announcedRun.current = { conversationId, runId: event.runId };
    setValue({ conversationId, cluster: live });
  }, [conversationId, event.phase, event.runId, live]);
  return value?.conversationId === conversationId ? value.cluster : null;
}

interface MetricProps {
  readonly label: string;
  readonly value: string;
  readonly session: string;
  readonly sessionLabel: string;
}

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
