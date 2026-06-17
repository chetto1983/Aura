import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { readReasoningPref, writeReasoningPref } from './reasoningPref';

// ReasoningDrawer (D-01): a collapsible chain-of-thought part. The cockpit SSE
// path streams REASONING_* deltas (server.go flip, plan 25-01); this surfaces
// them in a drawer whose show/hide preference is persisted (builder default =
// shown). The reasoning *trace* still does not persist verbatim (HARDEN-05) —
// this is the LIVE cockpit stream, not trace storage.

export interface ReasoningDrawerProps {
  /** The accumulated reasoning text for this assistant turn. */
  readonly text: string;
}

export function ReasoningDrawer({ text }: ReasoningDrawerProps) {
  const { t } = useTranslation();
  const [shown, setShown] = useState<boolean>(readReasoningPref);

  const toggle = useCallback(() => {
    setShown((prev) => {
      const next = !prev;
      writeReasoningPref(next);
      return next;
    });
  }, []);

  if (text.length === 0) return null;

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-surface-2">
      <button
        type="button"
        onClick={toggle}
        aria-pressed={shown}
        aria-expanded={shown}
        aria-controls="reasoning-body"
        className="flex min-h-[var(--row-h)] w-full items-center gap-2 px-3 py-1 text-xs font-medium text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
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
          className={`shrink-0 transition-transform motion-reduce:transition-none ${shown ? 'rotate-90' : ''}`}
        >
          <path d="m9 18 6-6-6-6" />
        </svg>
        <span>{shown ? t('chat.reasoning.hide') : t('chat.reasoning.show')}</span>
      </button>
      {shown ? (
        <div
          id="reasoning-body"
          className="whitespace-pre-wrap border-t border-border px-3 py-2 text-xs leading-relaxed text-text-muted"
        >
          {text}
        </div>
      ) : null}
    </div>
  );
}
