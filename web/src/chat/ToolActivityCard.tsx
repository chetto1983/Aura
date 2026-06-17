import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toolStatus, type ToolStatus } from './toolStatus';

// ToolActivityCard (D-02): the LIGHTWEIGHT raw tool-activity view. It shows the
// tool name + a status dot (icon + text, never colour alone) + an expandable
// mono raw text/JSON result blob.
//
// SECURITY (T-25-11 / HARDEN-08): the raw blob is untrusted tool/swarm output —
// it renders as TEXT inside a <pre> (React escapes children), never as raw HTML,
// and is NEVER passed through a markdown renderer. There is deliberately NO typed
// per-type display routing here — that is Phase 26 (DISP-01..05). The XSS guard is
// asserted behaviourally in ToolActivityCard.test.tsx.

const DOT_CLASS: Record<ToolStatus, string> = {
  running: 'bg-warning',
  done: 'bg-success',
  error: 'bg-danger',
};

const RULE_CLASS: Record<ToolStatus, string> = {
  running: 'border-l-warning',
  done: 'border-l-success',
  error: 'border-l-danger',
};

const PILL_CLASS: Record<ToolStatus, string> = {
  running: 'border-warning/50 text-warning',
  done: 'border-success/50 text-success',
  error: 'border-danger/50 text-danger',
};

export interface ToolActivityCardProps {
  readonly toolName: string;
  /** Raw streamed argument text (partial JSON during streaming). */
  readonly argsText?: string;
  /** Raw tool-result preview, when the call has completed. */
  readonly result?: string;
  readonly isError?: boolean;
}

export function ToolActivityCard({ toolName, argsText, result, isError }: ToolActivityCardProps) {
  const { t } = useTranslation();
  const bodyId = useId();
  const status = toolStatus({
    ...(result !== undefined ? { result } : {}),
    ...(isError !== undefined ? { isError } : {}),
  });
  // The raw blob: prefer the result; while running, show the streamed args so the
  // operator sees what was requested. Always rendered as plain text/mono.
  const raw = result ?? argsText ?? '';
  const hasRaw = raw.length > 0;
  const userToggled = useRef(false);
  const [expanded, setExpanded] = useState(status === 'running' && hasRaw);

  useEffect(() => {
    if (userToggled.current) return;
    setExpanded(status === 'running' && hasRaw);
  }, [status, hasRaw]);

  return (
    <div
      className={`rounded-[var(--radius-md)] border border-l-2 border-border bg-surface-2 ${RULE_CLASS[status]}`}
    >
      <div className="flex min-h-[var(--row-h)] items-center justify-between gap-2 px-3 py-1">
        <span className="flex items-center gap-2">
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${DOT_CLASS[status]}`}
          />
          <span className="font-mono text-xs text-text">{toolName}</span>
          <span
            className={`rounded-[var(--radius-pill)] border px-2 py-0.5 text-[0.6875rem] ${PILL_CLASS[status]}`}
          >
            {t(`chat.tool.status.${status}`)}
          </span>
        </span>
        {hasRaw ? (
          <button
            type="button"
            onClick={() => {
              userToggled.current = true;
              setExpanded((v) => !v);
            }}
            aria-expanded={expanded}
            aria-controls={bodyId}
            aria-label={expanded ? t('chat.tool.hideRaw') : t('chat.tool.showRaw')}
            className="flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-md)] px-2 text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
              className={`transition-transform motion-reduce:transition-none ${expanded ? 'rotate-180' : ''}`}
            >
              <path d="m6 9 6 6 6-6" />
            </svg>
          </button>
        ) : null}
      </div>
      {expanded && hasRaw ? (
        <pre
          id={bodyId}
          className="overflow-x-auto border-t border-border px-3 py-2 font-mono text-xs leading-relaxed text-text-muted"
        >
          {raw}
        </pre>
      ) : null}
    </div>
  );
}
