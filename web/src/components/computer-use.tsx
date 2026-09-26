import type { ComponentProps, ReactNode } from 'react';
import { MousePointer2 } from 'lucide-react';
import { cn } from '@/lib/utils';

// Owned copy of the @assistant-ui/elements-computer-use registry item. Changes from the
// installed source: the element fits its screen instead of a fixed max-w-md and a minimum
// height, and the address bar and the action line are contained so a long URL cannot widen
// it: the cursor positions are percentages of the screen box, so that box must be exactly the
// page the caller renders, never letterboxing around it. The step counter is gone (a live view
// has no known total), and the surfaces read the cockpit theme tokens instead of the
// elements-surfaces classes and the tw-shimmer plugin, which nothing here used.

export interface ComputerStep {
  readonly id: string;
  readonly action: string;
  readonly target: string;
  /** Percent of the screen width, 0-100. */
  readonly x: number;
  /** Percent of the screen height, 0-100. */
  readonly y: number;
}

export interface ComputerUseProps extends Omit<ComponentProps<'div'>, 'children'> {
  readonly url: string;
  /** The last one is the active step; the two before it draw the trail. */
  readonly steps: readonly ComputerStep[];
  /** The screen: its box is the coordinate space of every step. */
  readonly children: ReactNode;
}

const TRAIL = 3;
const TINTS = ['bg-danger/50', 'bg-warning/50', 'bg-success/50'];

export function ComputerUse({ url, steps, children, className, ...props }: ComputerUseProps) {
  const trail = steps.slice(-TRAIL);
  const active = trail.at(-1);

  return (
    <div
      data-slot="computer-use"
      className={cn(
        'flex w-fit max-w-full flex-col overflow-hidden rounded-2xl border border-border bg-surface',
        className,
      )}
      {...props}
    >
      <div className="flex items-center gap-2 px-3 py-2 [contain:inline-size]">
        <span aria-hidden className="flex shrink-0 gap-1">
          {TINTS.map((tint) => (
            <span key={tint} className={cn('size-2 rounded-full', tint)} />
          ))}
        </span>
        <span className="min-w-0 flex-1 truncate rounded-full bg-surface-2 px-2.5 py-1 font-mono text-[11px] tracking-tight text-text-muted">
          {url}
        </span>
      </div>

      <div className="relative overflow-hidden border-t border-border">
        {children}

        {trail.map((step, i) => (
          <span
            key={step.id}
            aria-hidden
            className="pointer-events-none absolute size-2 -translate-1/2 rounded-full bg-accent transition-opacity duration-300"
            style={{
              left: `${String(step.x)}%`,
              top: `${String(step.y)}%`,
              opacity: 0.18 * (i + 1),
            }}
          />
        ))}

        {active ? (
          <MousePointer2
            aria-hidden
            className="pointer-events-none absolute size-4 fill-accent stroke-accent transition-[left,top] duration-500 ease-out motion-reduce:transition-none"
            style={{ left: `${String(active.x)}%`, top: `${String(active.y)}%` }}
          />
        ) : null}
      </div>

      {active ? (
        <div className="flex items-center gap-2 border-t border-border px-3.5 py-2 [contain:inline-size]">
          <span className="shrink-0 font-mono text-[11px] tracking-tight text-text-muted">
            {active.action}
          </span>
          <span className="min-w-0 flex-1 truncate text-[13px] text-text">{active.target}</span>
        </div>
      ) : null}
    </div>
  );
}
