import { useTranslation } from 'react-i18next';
import { useConversationRotEvents } from '../conversations/useConversations';
import {
  contextPercent,
  formatTokens,
  isContextNearFull,
  totalPairsDropped,
} from './footerMetrics';

// ContextBudgetGauge (D-11/D-12) shows tokens-in-context vs the model window as a
// fill bar — "{{used}} / {{window}} · {{percent}}%". The used level rides the live
// STATE_DELTA prompt_tokens (already on the wire, no new route). The fill is the
// reserved accent (UI-SPEC accent item 7) below the near-full threshold, switching
// to `warning` at ≥85%. A microcompact event from GET /api/conversations/{id}/
// rot-events (pairs_dropped) renders the "Compacted N older turns" marker so the
// operator sees BOTH fullness and why older context dropped.
//
// This is a runtime instrument, NOT a typed display — it is kept entirely off the
// Phase-26 typed-display namespace (no payload-type routing).

export interface ContextBudgetGaugeProps {
  /** Tokens-in-context (live STATE_DELTA prompt_tokens of the latest turn). */
  readonly usedTokens: number;
  /** The model context window (DeepSeek-V4 default 1,000,000; AppShell may override). */
  readonly windowTokens: number;
  /** The open conversation; drives the rot-events read for the compaction marker. */
  readonly conversationId: string;
}

export function ContextBudgetGauge({
  usedTokens,
  windowTokens,
  conversationId,
}: ContextBudgetGaugeProps) {
  const { t } = useTranslation();
  const { data: rotEvents } = useConversationRotEvents(conversationId);
  const percent = contextPercent(usedTokens, windowTokens);
  const nearFull = isContextNearFull(percent);
  const dropped = totalPairsDropped(rotEvents ?? []);

  return (
    <div className="flex min-w-[10rem] flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[0.625rem] font-medium uppercase tracking-wider text-text-faint">
          {t('footer.context')}
        </span>
        <span className="font-mono text-xs text-text">
          {t('footer.gaugeValue', {
            used: formatTokens(usedTokens),
            window: formatTokens(windowTokens),
            percent,
          })}
        </span>
      </div>
      {/* The fill bar. role=progressbar + aria-valuenow carry the meaning so the
          colour is decorative only (WCAG 1.4.1). motion-reduce disables the fill
          transition (UI-SPEC reduced-motion contract). */}
      <div
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        aria-label={t('footer.contextLabel')}
        className="h-1.5 w-full overflow-hidden rounded-full bg-surface-2"
      >
        <div
          className={`h-full rounded-full transition-[width] motion-reduce:transition-none ${
            nearFull ? 'bg-warning' : 'bg-accent'
          }`}
          style={{ width: `${String(percent)}%` }}
        />
      </div>
      {dropped > 0 ? (
        <p className="font-mono text-[0.625rem] text-text-muted">
          {t('footer.compacted', { count: dropped })}
        </p>
      ) : null}
    </div>
  );
}
