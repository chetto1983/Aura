import { useTranslation } from 'react-i18next';
import type { DisplaySource } from './types';

// SourcesButton (D-13 / DISP-05): the answer-level "Sources (N)" affordance. It
// shows the count of consulted sources and opens the read-only Source Explorer on
// click/Enter/tap (a native <button>, never hover-only). N=0 hides the button
// entirely (the empty-state is silence — typed displays are additive, UI-SPEC).
//
// COLOR (UI-SPEC Color rule #4): the "Sources (N)" text is accent — it is the
// answer-level navigation into the evidence dossier.

export interface SourcesButtonProps {
  /** The consulted-source registry for this answer (the same one the sheet renders). */
  readonly sources: readonly DisplaySource[];
  /** Opens the shared Source Explorer sheet over `sources` (no focusRefId). */
  readonly onOpen: (sources: readonly DisplaySource[]) => void;
}

export function SourcesButton({ sources, onOpen }: SourcesButtonProps) {
  const { t } = useTranslation();
  const count = sources.length;
  if (count === 0) return null; // N=0 → no button (silent empty-state)

  return (
    <button
      type="button"
      onClick={() => {
        onOpen(sources);
      }}
      aria-label={t('source.sourcesAria', { count })}
      className="inline-flex min-h-11 items-center gap-1 rounded-[var(--radius-md)] px-2 text-[0.75rem] font-medium text-accent-text outline-none hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
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
      >
        <path d="M4 6h16 M4 12h16 M4 18h10" />
      </svg>
      {t('source.sources', { count })}
    </button>
  );
}
