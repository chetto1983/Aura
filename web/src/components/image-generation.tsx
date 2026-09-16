import type { ComponentProps, ReactNode } from 'react';
import { cn } from '@/lib/utils';

// Owned copy of the @assistant-ui/elements-image-generation registry item. Changes from the
// installed source: the frame takes the requested aspect ratio instead of a fixed square and
// a fixed "1024 × 1024" label; the status line and the corner readout are props, so the
// cockpit localizes them; the prompt stays visible while generating; the regenerate button
// is gone (it had no handler, and the message action bar's Reload already regenerates); the
// gradient reads the cockpit theme tokens so it follows light and dark. The three pieces it
// took from elements-surfaces (the paper and mono classes, the shimmer label) live here: the
// shimmer is the cockpit's aura-thinking-shimmer (styles/motion.css), which already drops to
// a plain label under prefers-reduced-motion, instead of the tw-shimmer plugin.

const DOTS = Array.from({ length: 64 }, (_, i) => i);

const SETTLED_GRADIENT = [
  'radial-gradient(120% 90% at 20% 100%, var(--color-accent-pressed) 0%, transparent 55%)',
  'radial-gradient(110% 80% at 85% 90%, color-mix(in oklab, var(--color-info) 45%, transparent) 0%, transparent 60%)',
  'radial-gradient(130% 100% at 60% 0%, color-mix(in oklab, var(--color-accent-text) 35%, transparent) 0%, transparent 75%)',
  'linear-gradient(to top, var(--color-surface-2), var(--color-surface-3))',
].join(', ');

// The frame is at most this tall; a portrait ratio narrows it instead of growing.
const MAX_FRAME_HEIGHT = '20rem';

export interface ImageGenerationProps extends Omit<ComponentProps<'div'>, 'children' | 'prompt'> {
  readonly prompt: string;
  readonly generating: boolean;
  /** A CSS aspect-ratio value such as "16 / 9". */
  readonly aspectRatio: string;
  /** The status line under the frame, shimmering while generating. */
  readonly label: string;
  /** A readout in the frame's corner, such as the elapsed time. */
  readonly meta?: ReactNode;
}

export function ImageGeneration({
  prompt,
  generating,
  aspectRatio,
  label,
  meta,
  className,
  ...props
}: ImageGenerationProps) {
  return (
    <div
      data-slot="image-generation"
      className={cn('flex w-full max-w-md flex-col gap-2.5', className)}
      {...props}
    >
      <div
        data-slot="generation-visual"
        className="relative overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface"
        style={{ aspectRatio, width: `min(100%, calc(${MAX_FRAME_HEIGHT} * (${aspectRatio})))` }}
      >
        <div
          aria-hidden="true"
          className="absolute inset-0 grid grid-cols-8 place-items-center p-6"
        >
          {DOTS.map((dot) => (
            <span
              key={dot}
              className={cn(
                'size-1 rounded-full bg-text-faint/50 transition-opacity duration-500',
                generating ? 'animate-pulse motion-reduce:animate-none' : 'opacity-0',
              )}
              style={{ animationDelay: `${String((Math.floor(dot / 8) + (dot % 8)) * 90)}ms` }}
            />
          ))}
        </div>
        <div
          aria-hidden="true"
          className={cn(
            'absolute inset-0 transition-[opacity,filter] duration-1000 ease-out motion-reduce:transition-none',
            generating ? 'opacity-0 blur-xl' : 'opacity-100 blur-0',
          )}
          style={{ background: SETTLED_GRADIENT }}
        />
        {meta === undefined ? null : (
          <span className="absolute end-2.5 top-2.5 font-mono text-[11px] tracking-tight tabular-nums text-text-muted">
            {meta}
          </span>
        )}
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        <span
          role="status"
          className={cn(
            'text-xs font-medium text-text-muted',
            generating && 'aura-thinking-shimmer',
          )}
        >
          {label}
        </span>
        <p className="text-sm text-text [overflow-wrap:anywhere]">{prompt}</p>
      </div>
    </div>
  );
}
