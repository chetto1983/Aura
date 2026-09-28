// Adapted from @assistant-ui/elements-terminal-block, https://r.assistant-ui.com/elements-terminal-block.json
// Registry snapshot fetched 2026-09-28, original SHA-256 971B7341A8C113C57C54174FEE0004425ACC4B63CBD68416FF19B1B9BFBD7866.
// MIT license and copyright notice: see LICENSE.assistant-ui in this directory.
'use client';

import { useState, type ComponentProps } from 'react';
import { CheckIcon, Loader2Icon, XIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

export function TerminalBlock({
  command,
  lines,
  visibleCount,
  done,
  cwd,
  exitCode,
  durationMs,
  truncated = false,
  maxCollapsedLines = 12,
  showAllLabel = 'Show all',
  showLessLabel = 'Show less',
  truncatedLabel = 'Output truncated',
  className,
  ...props
}: Omit<ComponentProps<'div'>, 'children'> & {
  command: string;
  lines: readonly string[];
  visibleCount: number;
  done: boolean;
  cwd?: string;
  exitCode?: number;
  durationMs?: number;
  truncated?: boolean;
  maxCollapsedLines?: number;
  showAllLabel?: string;
  showLessLabel?: string;
  truncatedLabel?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const visible = lines.slice(0, Math.max(0, visibleCount));
  const collapsed = done && !expanded && visible.length > maxCollapsedLines;
  const shown = collapsed ? visible.slice(0, maxCollapsedLines) : visible;
  const failed = done && exitCode !== undefined && exitCode !== 0;

  return (
    <div
      data-slot="terminal-block"
      data-state={!done ? 'running' : failed ? 'failed' : 'done'}
      className={cn(
        'w-full min-w-0 overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface font-mono text-xs',
        className,
      )}
      {...props}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 border-b border-border px-3 py-2">
        {cwd ? <span className="max-w-full break-all text-text-muted">{cwd}</span> : null}
        <span aria-hidden className="text-text-faint">
          $
        </span>
        <span className="min-w-0 flex-1 break-all text-text">{command}</span>
        {done ? (
          <span
            className={cn(
              'flex items-center gap-1 whitespace-nowrap',
              failed ? 'text-red-600 dark:text-red-400' : 'text-text-muted',
            )}
          >
            {failed ? (
              <XIcon aria-hidden className="size-3" />
            ) : (
              <CheckIcon aria-hidden className="size-3" />
            )}
            exit {exitCode}
          </span>
        ) : (
          <Loader2Icon
            aria-hidden
            className="size-3 animate-spin text-text-muted motion-reduce:animate-none"
          />
        )}
        {durationMs !== undefined ? (
          <span className="whitespace-nowrap text-text-faint">{durationMs}ms</span>
        ) : null}
      </div>
      <div className="min-w-0 px-3 py-2 text-text-muted">
        {shown.map((line, index) => (
          <div key={index} className="whitespace-pre-wrap break-all">
            {line || '\u00a0'}
          </div>
        ))}
        {truncated ? <p className="pt-1 text-text-faint">{truncatedLabel}</p> : null}
        {!done ? (
          <span
            aria-hidden
            className="inline-block h-3 w-1.5 animate-pulse bg-blue-500 motion-reduce:animate-none"
          />
        ) : null}
      </div>
      {done && visible.length > maxCollapsedLines ? (
        <button
          type="button"
          className="w-full border-t border-border px-3 py-2 text-left text-accent-text hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-accent"
          onClick={() => {
            setExpanded((value) => !value);
          }}
          aria-expanded={expanded}
        >
          {expanded ? showLessLabel : showAllLabel}
        </button>
      ) : null}
    </div>
  );
}
