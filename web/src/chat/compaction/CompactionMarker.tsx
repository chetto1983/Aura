import { useState } from 'react';
import { ChevronDown, Scissors } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatTokens } from '../footerMetrics';
import type { CompactionState } from './api';

// CompactionMarker — where the transcript stops being what the model reads.
//
// Everything above this line is replayed to the model as ONE summary; everything below it is
// replayed verbatim. That boundary existed long before this component (the ladder has been
// compacting at half the window since 2026-08-16) and was invisible: the operator saw a full
// transcript and had no way to tell which part of it the model could still see. A footer
// counter cannot say it either — the fact is positional.
//
// It is a marker, not a message: the summary is disclosed on demand rather than rendered
// inline, because it is machine-facing text and putting a page of it in the middle of the
// conversation would bury the turns it sits between.

export interface CompactionMarkerProps {
  readonly state: CompactionState;
}

export function CompactionMarker({ state }: CompactionMarkerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const before = state.tokens_before ?? 0;
  const after = state.tokens_after ?? 0;
  const showSaved = before > 0 && after > 0;

  return (
    <section
      data-testid="compaction-marker"
      aria-label={t('chat.compaction.marker')}
      className="my-2 flex flex-col gap-2"
    >
      <div className="flex items-center gap-3">
        {/* The rules are decorative: the meaning is in the label between them, which is why
            they are aria-hidden rather than an <hr> a screen reader would announce twice. */}
        <span aria-hidden="true" className="h-px flex-1 bg-border" />
        <button
          type="button"
          onClick={() => {
            setOpen((wasOpen) => !wasOpen);
          }}
          aria-expanded={open}
          disabled={state.summary.length === 0}
          className="group flex items-center gap-2 rounded-full border border-border bg-surface-2 px-3 py-1 text-[0.75rem] text-text-muted transition-colors hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-default disabled:hover:text-text-muted"
        >
          <Scissors data-icon aria-hidden="true" className="size-3.5 text-accent-text" />
          <span className="font-medium text-text">{t('chat.compaction.marker')}</span>
          {state.source_turns > 0 ? (
            <span className="hidden sm:inline">
              ·{' '}
              {t('chat.compaction.markerDetail', {
                count: state.source_turns,
              })}
            </span>
          ) : null}
          {showSaved ? (
            <span className="font-mono [font-variant-numeric:tabular-nums]">
              ·{' '}
              {t('chat.compaction.saved', {
                before: formatTokens(before),
                after: formatTokens(after),
              })}
            </span>
          ) : null}
          {state.summary.length > 0 ? (
            <ChevronDown
              data-icon
              aria-hidden="true"
              className={`size-3.5 transition-transform motion-reduce:transition-none ${open ? 'rotate-180' : ''}`}
            />
          ) : null}
        </button>
        <span aria-hidden="true" className="h-px flex-1 bg-border" />
      </div>
      {open && state.summary.length > 0 ? (
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface-2 p-3">
          <p className="whitespace-pre-wrap text-[0.8125rem] leading-relaxed text-text-muted">
            {state.summary}
          </p>
          <p className="mt-2 text-[0.75rem] text-text-faint">{t('chat.compaction.kept')}</p>
        </div>
      ) : null}
    </section>
  );
}
