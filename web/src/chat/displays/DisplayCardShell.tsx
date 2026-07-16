import type { ReactNode } from 'react';

// DisplayCardShell — the shared chrome for every typed display card (26-04/26-05).
// It gives structured result cards their own rounded surface, semantic left rule,
// type header, optional metadata/actions, and slotted body. This typed result layer
// complements the lightweight raw ToolActivityCard fallback (D-02). Accent stays
// scarce here — the shell is neutral surface/border/text (Color rule).
//
// Kept dependency-free and side-effect-free so it composes inside any per-type card
// without pulling pagination/i18n into the shell itself (each body owns its own t()).

export interface DisplayCardShellProps {
  /** The translated type label (e.g. "Table", "Chart") shown as the card header. */
  readonly label: string;
  /** Optional micro-meta beside the label (item count, lang, severity word). */
  readonly meta?: ReactNode;
  /** Optional right-aligned controls (copy / export / filter chips). */
  readonly actions?: ReactNode;
  /** The semantic left-rule tint; defaults to the neutral border. */
  readonly rule?: 'neutral' | 'danger' | 'warning' | 'info' | 'success';
  /** The per-type body. */
  readonly children: ReactNode;
}

const RULE_CLASS: Record<NonNullable<DisplayCardShellProps['rule']>, string> = {
  neutral: 'border-l-border',
  danger: 'border-l-danger',
  warning: 'border-l-warning',
  info: 'border-l-info',
  success: 'border-l-success',
};

export function DisplayCardShell({
  label,
  meta,
  actions,
  rule = 'neutral',
  children,
}: DisplayCardShellProps) {
  return (
    <div
      className={`rounded-[var(--radius-md)] border border-l-2 border-border bg-surface-2 ${RULE_CLASS[rule]}`}
    >
      <div className="flex min-h-[var(--row-h)] flex-wrap items-center justify-between gap-2 px-3 py-2">
        <span className="flex items-center gap-2">
          <span className="text-xs font-medium text-text">{label}</span>
          {meta !== undefined ? (
            <span className="text-[0.75rem] text-text-faint">{meta}</span>
          ) : null}
        </span>
        {actions !== undefined ? (
          <span className="flex flex-wrap items-center gap-1">{actions}</span>
        ) : null}
      </div>
      <div className="border-t border-border p-4">{children}</div>
    </div>
  );
}
