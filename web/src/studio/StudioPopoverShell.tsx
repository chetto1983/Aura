import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

// StudioPopoverShell — the chrome both composer popovers share: the pill that opens them, the
// heading that names what is inside, and the Reset that puts those controls back to the
// cheapest declared values. Extracted because the two bodies differ only in their controls,
// and two copies of this frame is exactly the clone the duplication gate refuses.

interface StudioPopoverShellProps {
  /** What the pill reads — the summary for Options, an icon for Advanced. */
  readonly pill: ReactNode;
  /** The pill's accessible name, which a summary or an icon alone does not give. */
  readonly pillLabel: string;
  readonly title: string;
  readonly onReset: () => void;
  readonly children: ReactNode;
}

export function StudioPopoverShell({
  pill,
  pillLabel,
  title,
  onReset,
  children,
}: StudioPopoverShellProps) {
  const { t } = useTranslation();
  return (
    <Popover>
      <PopoverTrigger className="studio-pill" aria-label={pillLabel}>
        {pill}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[min(22.5rem,calc(100vw-2rem))] rounded-[var(--radius-md)] border-border bg-surface p-3 shadow-[var(--shadow-popover)]"
      >
        <div className="mb-3 flex items-center justify-between gap-2">
          <h2 className="text-[13px] font-semibold text-text">{title}</h2>
          <button
            type="button"
            onClick={onReset}
            className="rounded-[var(--radius-sm)] px-1.5 py-0.5 text-[11px] text-text-faint underline-offset-2 hover:text-text hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            {t('studio.options.reset')}
          </button>
        </div>
        <div className="flex flex-col gap-3.5">{children}</div>
      </PopoverContent>
    </Popover>
  );
}

/** One labelled block inside a popover. */
export function StudioField({
  label,
  hint,
  children,
}: {
  readonly label: string;
  readonly hint?: string;
  readonly children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[11px] tracking-wide text-text-faint uppercase">{label}</span>
        {hint === undefined ? null : (
          <span className="text-[11px] text-text-muted tabular-nums">{hint}</span>
        )}
      </div>
      {children}
    </div>
  );
}
