// Adapted from @assistant-ui/elements-code-diff, https://r.assistant-ui.com/elements-code-diff.json
// Registry snapshot fetched 2026-09-28, original SHA-256 4AAC7D976A391479AACD0B4A8A0539A3691D0B6556B94845A856BF463349D3EF.
// MIT license and copyright notice: see LICENSE.assistant-ui in this directory.
'use client';

import type { ComponentProps } from 'react';
import { cn } from '@/lib/utils';

export type DiffKind = 'context' | 'added' | 'removed';
export interface DiffLine {
  kind: DiffKind;
  text: string;
}

const GUTTER: Record<DiffKind, string> = { context: ' ', added: '+', removed: '−' };

export function CodeDiff({
  filename,
  additions,
  deletions,
  lines,
  cycle,
  className,
  ...props
}: Omit<ComponentProps<'div'>, 'children'> & {
  filename: string;
  additions: number;
  deletions: number;
  lines: readonly DiffLine[];
  cycle: number;
}) {
  return (
    <div
      data-slot="code-diff"
      className={cn(
        'w-full min-w-0 overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface font-mono text-xs',
        className,
      )}
      {...props}
    >
      <div className="flex min-w-0 items-center justify-between gap-2 px-3 py-2">
        <span className="min-w-0 truncate text-text" title={filename}>
          {filename}
        </span>
        <span className="flex shrink-0 gap-2 tabular-nums">
          <span className="text-emerald-700 dark:text-emerald-400">+{additions}</span>
          <span className="text-red-600 dark:text-red-400">−{deletions}</span>
        </span>
      </div>
      <div className="max-w-full overflow-x-auto border-t border-border">
        <div className="w-max min-w-full py-1">
          {lines.map((line, index) => (
            <div
              key={`${String(cycle)}-${String(index)}-${line.text}`}
              className={cn(
                'fade-in animate-in fill-mode-both flex px-3 py-0.5 leading-relaxed whitespace-pre duration-300 motion-reduce:animate-none',
                line.kind === 'context' && 'text-text-muted',
                line.kind === 'added' && 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
                line.kind === 'removed' && 'bg-red-500/10 text-red-700 dark:text-red-300',
              )}
              style={{ animationDelay: `${String(Math.min(index, 20) * 30)}ms` }}
            >
              <span aria-hidden className="w-4 shrink-0 select-none">
                {GUTTER[line.kind]}
              </span>
              <span>{line.text || '\u00a0'}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
