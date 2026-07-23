import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatElapsed } from './durationFormat';
import { Button } from '@/components/ui/button';

// ReasoningPill — the compact chain-of-thought affordance (compact-chat spec
// docs/superpowers/specs/2026-07-23-cockpit-compact-chat-ui-spec.md §2.2):
// one 44px ghost row per reasoning span, COLLAPSED BY DEFAULT ALWAYS, per-part
// ephemeral expand state (useState — the persisted aura.chat.reasoning.shown
// preference is retired, §2.4). States:
//   streaming        → shimmer "Thinking…" + aria-hidden last-line ticker (§2.2/OQ-3)
//   done, duration   → "Thought for X s" (live span timestamps OR snapshot durationMs)
//   done, unknown    → "Reasoning" fallback label (pre-migration rows)
// Expanded body: muted sans prose behind an inset rule, max-h-80 scroll container;
// while streaming it live-appends and follows the tail unless the operator
// scrolled up. No per-pill live region — the thread role="status" row announces.

export interface ReasoningPillProps {
  /** The accumulated reasoning text for this span. */
  readonly text: string;
  /** Epoch-ms live-stream span timestamps (REASONING_* frames). */
  readonly startedAt?: number | undefined;
  readonly finishedAt?: number | undefined;
  /** Persisted wallclock from the snapshot's reasoningDurationMs decoration. */
  readonly durationMs?: number | undefined;
  /** True while the parent assistant message is still running. */
  readonly isRunning?: boolean | undefined;
}

/** The last non-empty line of the streamed text — the §2.2 ticker content. */
function lastLine(text: string): string {
  for (let i = text.length - 1; i >= 0; ) {
    const start = text.lastIndexOf('\n', i);
    const line = text.slice(start + 1, i + 1).trim();
    if (line.length > 0) return line;
    if (start < 0) break;
    i = start - 1;
  }
  return '';
}

export function ReasoningPill({
  text,
  startedAt,
  finishedAt,
  durationMs,
  isRunning = false,
}: ReasoningPillProps) {
  const { t, i18n } = useTranslation();
  const bodyId = useId();
  const [expanded, setExpanded] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const followTail = useRef(true);

  const streaming = isRunning && finishedAt === undefined;
  const elapsedMs =
    startedAt !== undefined && finishedAt !== undefined && finishedAt > startedAt
      ? finishedAt - startedAt
      : durationMs;

  // While streaming, the expanded body follows the newest delta unless the
  // operator scrolled up (checked against the pre-append scroll position).
  useEffect(() => {
    const body = bodyRef.current;
    if (!expanded || !streaming || body === null) return;
    if (followTail.current) body.scrollTop = body.scrollHeight;
  }, [text, expanded, streaming]);

  const onBodyScroll = useCallback(() => {
    const body = bodyRef.current;
    if (body === null) return;
    followTail.current = body.scrollHeight - body.scrollTop - body.clientHeight < 24;
  }, []);

  const toggle = useCallback(() => {
    setExpanded((prev) => {
      const next = !prev;
      // Collapse while focus is inside the body (e.g. after keyboard-scrolling)
      // → return focus to the trigger so it never falls to <body> (§8.3).
      if (!next) {
        const active = document.activeElement;
        if (active !== null && bodyRef.current?.contains(active) === true) {
          rootRef.current?.querySelector('button')?.focus();
        }
      }
      return next;
    });
  }, []);

  // Empty degrade: a settled span with no text renders nothing (the snapshot
  // path never builds an empty part — this is the defensive live-path mirror).
  if (!streaming && text.length === 0) return null;

  const label = streaming
    ? t('chat.reasoning.thinking')
    : elapsedMs !== undefined
      ? t('chat.reasoning.thought', {
          duration: formatElapsed(elapsedMs, i18n.resolvedLanguage ?? i18n.language, t),
        })
      : t('chat.reasoning.label');
  const ticker = streaming ? lastLine(text) : '';

  return (
    <div ref={rootRef} data-testid="reasoning-pill">
      <Button
        type="button"
        variant="ghost"
        onClick={toggle}
        aria-expanded={expanded}
        aria-controls={bodyId}
        aria-label={expanded ? t('chat.reasoning.collapseAria') : t('chat.reasoning.expandAria')}
        className="min-h-11 w-full justify-start px-2 text-xs text-text-muted hover:text-text"
      >
        <ChevronRight
          data-icon
          aria-hidden="true"
          className={`transition-transform motion-reduce:transition-none ${expanded ? 'rotate-90' : ''}`}
        />
        <span className={streaming ? 'aura-thinking-shimmer' : undefined}>{label}</span>
      </Button>
      {ticker.length > 0 && !expanded ? (
        <p
          aria-hidden="true"
          data-testid="reasoning-ticker"
          className="line-clamp-1 ps-8 text-xs text-text-faint"
        >
          {ticker}
        </p>
      ) : null}
      <div
        id={bodyId}
        ref={bodyRef}
        hidden={!expanded}
        onScroll={onBodyScroll}
        role="region"
        aria-label={t('chat.reasoning.regionAria')}
        data-testid="reasoning-body"
        className="aura-part-reveal max-h-80 overflow-y-auto whitespace-pre-wrap border-s border-border px-3 py-2 text-xs leading-relaxed text-text-muted"
      >
        {text.length > 0 ? text : t('chat.reasoning.pending')}
      </div>
    </div>
  );
}
