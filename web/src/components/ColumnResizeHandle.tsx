import type { ColumnResizeControl } from './useColumnResize';
import { cn } from '@/lib/utils';

// The drag handle for a useColumnResize column. It is a <button> carrying role="separator" —
// the WAI-ARIA window-splitter pattern wants a FOCUSABLE separator, and starting from a
// natively-focusable element is what keeps the keyboard contract from rotting into an
// unreachable div. Drag it, or focus it and use the arrow keys (Shift for a bigger step,
// Home/End for the bounds). It is `lg`-only: below that breakpoint neither column exists.
export interface ColumnResizeHandleProps {
  readonly resize: ColumnResizeControl;
  readonly label: string;
  readonly className?: string;
}

// The lint rule below has no model for the splitter pattern and reads "separator" as decorative.
/* eslint-disable jsx-a11y/no-interactive-element-to-noninteractive-role */
export function ColumnResizeHandle({ resize, label, className }: ColumnResizeHandleProps) {
  return (
    <button
      type="button"
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={resize.width}
      aria-valuemin={resize.min}
      aria-valuemax={resize.max}
      onPointerDown={resize.onPointerDown}
      onKeyDown={resize.onKeyDown}
      className={cn(
        // z-10: the pane on the right is itself positioned and comes later in the DOM, so
        // without it that pane wins the hit test over the handle's own hit area.
        'relative z-10 hidden lg:block lg:w-px lg:shrink-0 lg:cursor-col-resize lg:bg-border lg:outline-none lg:ring-inset',
        // A 1px line is a 1px drag target: the pointer lands on the pane beside it and the
        // drag never starts. The hairline stays hairline and an invisible pseudo-element
        // carries the hit area (the chat rail's handle does the same, at `after:w-1`).
        'after:absolute after:inset-y-0 after:left-1/2 after:w-3 after:-translate-x-1/2 after:content-[""]',
        'hover:lg:bg-border-strong focus-visible:lg:ring-2 focus-visible:lg:ring-ring',
        className,
      )}
    />
  );
}
/* eslint-enable jsx-a11y/no-interactive-element-to-noninteractive-role */
