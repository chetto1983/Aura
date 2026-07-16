import { useEffect, useMemo, useRef, useState } from 'react';
import { ChevronDown } from 'lucide-react';
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
import type { RunUsageEvent } from './runUsage';
import { Button } from '@/components/ui/button';

export interface RuntimeFooterProps {
  readonly usageState: RunUsageEvent;
  readonly conversationId: string;
  readonly windowTokens?: number;
}

interface NumericCluster {
  readonly tokens: string;
  readonly cache: string;
  readonly cost: string;
  readonly sessionTokens: string;
  readonly sessionCache: string;
  readonly sessionCost: string;
}

export function RuntimeFooter({ usageState, conversationId, windowTokens }: RuntimeFooterProps) {
  const { t } = useTranslation();
  const { data: conv } = useConversation(conversationId);
  const [expanded, setExpanded] = useState(false);
  const turn = usageState.usage ?? null;
  const seed = seedSession(conv);
  const session = turn ? addTurn(seed, turn) : seed;
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
  const settled = useSettledAnnouncement(liveCluster, usageState);
  const usedTokens = turn?.promptTokens ?? session.promptTokens;
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
          aria-controls="footer-telemetry-detail"
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
          id="footer-telemetry-detail"
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

function useSettledAnnouncement(live: NumericCluster, event: RunUsageEvent): NumericCluster | null {
  const [value, setValue] = useState<NumericCluster | null>(null);
  const announcedRun = useRef<number | null>(null);
  useEffect(() => {
    if (event.phase !== 'settled' || announcedRun.current === event.runId) return;
    announcedRun.current = event.runId;
    setValue(live);
  }, [event.phase, event.runId, live]);
  return value;
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
