import { useCallback, useId, useState } from 'react';
import { ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { readReasoningPref, writeReasoningPref } from './reasoningPref';
import { Button } from '@/components/ui/button';

// ReasoningDrawer (D-01): a collapsible chain-of-thought part. The cockpit SSE
// path streams REASONING_* deltas (server.go flip, plan 25-01); this surfaces
// them in a drawer whose show/hide preference is persisted (unsaved browser
// default hidden). The reasoning *trace* still does not persist verbatim
// (HARDEN-05) - this is the LIVE cockpit stream, not trace storage.

export interface ReasoningDrawerProps {
  /** The accumulated reasoning text for this assistant reasoning span. */
  readonly text: string;
}

export function ReasoningDrawer({ text }: ReasoningDrawerProps) {
  const { t } = useTranslation();
  const bodyId = useId();
  const [shown, setShown] = useState<boolean>(readReasoningPref);
  const displayText = text.length > 0 ? text : t('chat.reasoning.pending');

  const toggle = useCallback(() => {
    setShown((prev) => {
      const next = !prev;
      writeReasoningPref(next);
      return next;
    });
  }, []);

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-surface-2">
      <Button
        type="button"
        variant="ghost"
        onClick={toggle}
        aria-expanded={shown}
        aria-controls={bodyId}
        className="min-h-11 w-full justify-start px-2 text-xs text-text-muted hover:text-text"
      >
        <ChevronRight
          data-icon
          aria-hidden="true"
          className={`transition-transform motion-reduce:transition-none ${shown ? 'rotate-90' : ''}`}
        />
        <span>{shown ? t('chat.reasoning.hide') : t('chat.reasoning.show')}</span>
      </Button>
      <div
        id={bodyId}
        hidden={!shown}
        className="whitespace-pre-wrap border-s border-border px-3 py-2 text-xs leading-relaxed text-text-muted"
      >
        {displayText}
      </div>
    </div>
  );
}
