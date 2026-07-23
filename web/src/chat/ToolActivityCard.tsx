import { useEffect, useId, useRef, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useElapsed } from './durationFormat';
import { toolStatus, type ToolStatus } from './toolStatus';
import { Button } from '@/components/ui/button';

// ToolActivityCard (D-02): the LIGHTWEIGHT raw tool-activity view. It shows a
// marker + tool name + text status + optional elapsed time + a raw-text disclosure.
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
}

interface ToolActivityRowProps {
  readonly toolName: string;
  readonly status: ToolStatus;
  readonly argsText?: string;
  readonly result?: string;
  /** Pre-formatted elapsed readout (parent only); omitted on child rows. */
  readonly elapsed?: string | null;
  /** Running ticks stay hidden from assistive technology; settled duration does not. */
  readonly elapsedRunning?: boolean;
}

/**
 * One tool-activity line: marker + name + text status + optional elapsed + an XSS-safe
 * raw-text disclosure. Owns the expand state machine: auto-expand while
 * running, auto-collapse ONCE on the running → done|error settle edge, and a userToggled
 * intent-guard that lets a manual toggle win thereafter. Used by both the parent card and
 * its nested child rows (no duplication).
 */
function ToolActivityRow({
  toolName,
  status,
  argsText,
  result,
  elapsed,
  elapsedRunning = false,
}: ToolActivityRowProps) {
  const { t } = useTranslation();
  const bodyId = useId();
  // The raw blob: prefer the result; while running, show the streamed args so the operator
  // sees what was requested. Always rendered as plain text/mono.
  const raw = result ?? argsText ?? '';
  const hasRaw = raw.length > 0;
  const userToggled = useRef(false);
  const settledOnce = useRef(status !== 'running');
  const previous = useRef({ status, hasRaw });
  const [expanded, setExpanded] = useState(status === 'running' && hasRaw);

  // Expand when streamed raw first arrives, then auto-collapse ONCE on the running →
  // done|error transition. Manual intent wins thereafter, and a later status flicker does
  // not re-drive the expander.
  useEffect(() => {
    const delayedRaw = status === 'running' && hasRaw && !previous.current.hasRaw;
    const settled = previous.current.status === 'running' && status !== 'running';
    if (!userToggled.current && delayedRaw && !settledOnce.current) setExpanded(true);
    if (!userToggled.current && settled && !settledOnce.current) setExpanded(false);
    if (settled) settledOnce.current = true;
    previous.current = { status, hasRaw };
  }, [hasRaw, status]);

  return (
    <>
      <div className="flex min-h-[var(--row-h)] min-w-0 items-center justify-between gap-2 px-3 py-1">
        <span className="flex min-w-0 items-center gap-2">
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${DOT_CLASS[status]}`}
          />
          <span className="min-w-0 break-words font-mono text-xs text-text">{toolName}</span>
          <span className="shrink-0 text-xs text-text-muted">
            {t(`chat.tool.status.${status}`)}
          </span>
          {elapsed !== null && elapsed !== undefined ? (
            <span
              aria-hidden={elapsedRunning ? true : undefined}
              data-testid="tool-elapsed"
              className="font-mono text-[0.75rem] tabular-nums text-text-faint"
            >
              {elapsed}
            </span>
          ) : null}
        </span>
        {hasRaw ? (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => {
              userToggled.current = true;
              setExpanded((v) => !v);
            }}
            aria-expanded={expanded}
            aria-controls={bodyId}
            aria-label={expanded ? t('chat.tool.hideRaw') : t('chat.tool.showRaw')}
            className="min-h-11 min-w-11 text-text-muted hover:text-text"
          >
            <ChevronDown
              data-icon
              aria-hidden="true"
              className={`transition-transform motion-reduce:transition-none ${expanded ? 'rotate-180' : ''}`}
            />
          </Button>
        ) : null}
      </div>
      {hasRaw ? (
        <pre
          id={bodyId}
          hidden={!expanded}
          className="overflow-x-auto border-t border-border px-3 py-2 font-mono text-xs leading-relaxed text-text-muted"
        >
          {raw}
        </pre>
      ) : null}
    </>
  );
}

export function ToolActivityCard({
  toolName,
  argsText,
  result,
  isError,
  startedAt,
  finishedAt,
  childActivity,
}: ToolActivityCardProps) {
  const status = toolStatus({
    ...(result !== undefined ? { result } : {}),
    ...(isError !== undefined ? { isError } : {}),
  });
  const elapsed = useElapsed(startedAt, finishedAt, status === 'running');
  const elapsedRunning = status === 'running';
  const hasChildren = childActivity !== undefined && childActivity.length > 0;

  return (
    <div className="rounded-[var(--radius-md)] border border-border">
      <ToolActivityRow
        toolName={toolName}
        status={status}
        {...(argsText !== undefined ? { argsText } : {})}
        {...(result !== undefined ? { result } : {})}
        elapsed={elapsed}
        elapsedRunning={elapsedRunning}
      />
      {hasChildren ? (
        <div data-testid="tool-children" className="ms-3 mb-1 ps-4 border-l border-border">
          {childActivity.map((child, i) => (
            <ToolActivityRow
              // Swarm child entries arrive in stream order and have no stable id; the index
              // is a stable key within a single parent card's children list.
              key={`${child.toolName}-${String(i)}`}
              toolName={child.toolName}
              status={toolStatus(child)}
              {...(child.argsText !== undefined ? { argsText: child.argsText } : {})}
              {...(child.result !== undefined ? { result: child.result } : {})}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}
