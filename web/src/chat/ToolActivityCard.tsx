import { useCallback, useId, useRef, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { DisplayRouter } from './displays/DisplayRouter';
import type { DisplayPayload } from './displays/types';
import { useElapsed } from './durationFormat';
import { toolStatus, type ToolStatus } from './toolStatus';
import { ToolResultPanel } from './ToolResultPanel';
import { resultMeta, summarizeArgs } from './toolSummary';
import { Button } from '@/components/ui/button';

// ToolActivityCard — the compact disclosure tool row (compact-chat spec
// docs/superpowers/specs/2026-07-23-cockpit-compact-chat-ui-spec.md §3):
// a single 44px one-liner [dot · mono name · args summary · meta · duration ·
// chevron], COLLAPSED BY DEFAULT in every state — the auto-expand/auto-collapse
// machinery is deleted (C2). Expanding reveals the typed DisplayRouter card
// (evidence types) or the structured ToolResultPanel (raw), inside the row.
// Errors stay collapsed with a danger tint + a visible "Error" word (OQ-2).
//
// SECURITY (T-25-11 / HARDEN-08): summaries and raw bodies are untrusted
// tool/swarm output — they render as escaped React text (the panel's highlight
// path is Shiki's tokenize-as-text HTML), NEVER through a markdown renderer.

const DOT_CLASS: Record<ToolStatus, string> = {
  running: 'bg-warning aura-dot-pulse',
  done: 'bg-success',
  error: 'bg-danger',
};

/** A nested subagent / child tool entry (swarm fan-out). One level of nesting only. */
export interface ToolActivityChild {
  readonly toolName: string;
  readonly argsText?: string;
  readonly result?: string;
  readonly isError?: boolean;
}

export interface ToolActivityCardProps {
  readonly toolName: string;
  /** Raw streamed argument text (partial JSON during streaming). */
  readonly argsText?: string;
  /** Raw tool-result preview, when the call has completed. */
  readonly result?: string;
  readonly isError?: boolean;
  /** Epoch-ms when the tool call started (AG-UI TOOL_CALL_START). Optional. */
  readonly startedAt?: number;
  /** Epoch-ms when the tool call ended (AG-UI TOOL_CALL_END). Optional. */
  readonly finishedAt?: number;
  /** Nested subagent/child tool entries (swarm). Absent/empty → single-row shape. */
  readonly childActivity?: readonly ToolActivityChild[];
  /** Typed display payload — the expanded body renders the DisplayRouter card. */
  readonly display?: DisplayPayload;
  /** Citation click-through for the expanded evidence displays (D-04). */
  readonly onOpenSource?: (refId: string) => void;
}

export function ToolActivityCard({
  toolName,
  argsText,
  result,
  isError,
  startedAt,
  finishedAt,
  childActivity,
  display,
  onOpenSource,
}: ToolActivityCardProps) {
  const { t } = useTranslation();
  const bodyId = useId();
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [expanded, setExpanded] = useState(false);
  const status = toolStatus({
    ...(result !== undefined ? { result } : {}),
    ...(isError !== undefined ? { isError } : {}),
  });
  const running = status === 'running';
  const elapsed = useElapsed(startedAt, finishedAt, running);
  const summary = summarizeArgs(toolName, argsText ?? '');
  // Result meta appears only once settled (§3.1 — absent while running).
  const meta = running ? null : resultMeta(display, result);
  const hasChildren = childActivity !== undefined && childActivity.length > 0;

  const toggle = useCallback(() => {
    setExpanded((prev) => {
      const next = !prev;
      // Collapse while focus is inside the body (e.g. after Copy) → return
      // focus to the row trigger so it never falls to <body> (§8.3).
      if (!next) {
        const active = document.activeElement;
        const body = document.getElementById(bodyId);
        if (active !== null && body?.contains(active) === true) {
          rootRef.current?.querySelector('button')?.focus();
        }
      }
      return next;
    });
  }, [bodyId]);

  let metaText: string | null = null;
  if (meta !== null) {
    metaText =
      meta.text ??
      `${meta.prefix !== undefined ? `${meta.prefix} · ` : ''}${t(meta.key ?? '', { count: meta.count ?? 0 })}`;
  }

  return (
    <div
      ref={rootRef}
      data-testid="tool-row"
      className={`rounded-[var(--radius-md)] border ${
        status === 'error' ? 'border-danger/40' : 'border-border'
      }`}
    >
      <Button
        type="button"
        variant="ghost"
        onClick={toggle}
        aria-expanded={expanded}
        aria-controls={bodyId}
        aria-label={t('chat.tool.rowAria', { tool: toolName })}
        title={t(expanded ? 'chat.tool.collapseAria' : 'chat.tool.expandAria', {
          tool: toolName,
        })}
        className="min-h-11 w-full justify-between gap-2 px-3 text-xs"
      >
        <span className="flex min-w-0 items-center gap-2">
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${DOT_CLASS[status]}`}
          />
          <span className="min-w-0 shrink-0 break-words font-mono text-xs text-text">
            {toolName}
          </span>
          {summary.length > 0 ? (
            <span className="min-w-0 truncate text-xs text-text-muted">{summary}</span>
          ) : null}
          {/* The visible status WORD is dropped (dot + duration carry it); AT
              still hears it. Error stays visible — never color-only (1.4.1). */}
          <span className="sr-only">{t(`chat.tool.status.${status}`)}</span>
          {status === 'error' ? (
            <span className="shrink-0 text-xs font-medium text-danger">
              {t('chat.tool.status.error')}
            </span>
          ) : null}
        </span>
        <span className="flex shrink-0 items-center gap-2">
          {metaText !== null ? (
            <span data-testid="tool-meta" className="text-[0.75rem] text-text-faint">
              {metaText}
            </span>
          ) : null}
          {elapsed !== null ? (
            <span
              aria-hidden={running ? true : undefined}
              data-testid="tool-elapsed"
              className="font-mono text-[0.75rem] tabular-nums text-text-faint"
            >
              {elapsed}
            </span>
          ) : null}
          <ChevronDown
            data-icon
            aria-hidden="true"
            className={`transition-transform motion-reduce:transition-none ${expanded ? 'rotate-180' : ''}`}
          />
        </span>
      </Button>
      <div
        id={bodyId}
        hidden={!expanded}
        role="region"
        aria-label={t('chat.tool.regionAria', { tool: toolName })}
        data-testid="tool-body"
        className="aura-part-reveal border-t border-border"
      >
        {display !== undefined ? (
          <div className="p-2">
            <DisplayRouter
              payload={display}
              toolName={toolName}
              {...(argsText !== undefined ? { argsText } : {})}
              {...(result !== undefined ? { result } : {})}
              {...(isError !== undefined ? { isError } : {})}
              {...(onOpenSource !== undefined ? { onOpenSource } : {})}
            />
          </div>
        ) : (
          <ToolResultPanel argsText={argsText} result={result} />
        )}
        {hasChildren ? (
          <div data-testid="tool-children" className="ms-3 mb-1 border-l border-border ps-4">
            {childActivity.map((child, i) => (
              <ToolActivityCard
                // Swarm child entries arrive in stream order and have no stable id; the
                // index is a stable key within a single parent card's children list.
                key={`${child.toolName}-${String(i)}`}
                toolName={child.toolName}
                {...(child.argsText !== undefined ? { argsText: child.argsText } : {})}
                {...(child.result !== undefined ? { result: child.result } : {})}
                {...(child.isError !== undefined ? { isError: child.isError } : {})}
              />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}
