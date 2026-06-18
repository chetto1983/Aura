import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  HoverCard,
  HoverCardContent,
  HoverCardPortal,
  HoverCardTrigger,
} from '@radix-ui/react-hover-card';
import type { DisplayKind, DisplaySource } from './types';

// CitationBubble (D-04): the numbered citation chip. It is a real <button> (the
// inline deep-search signature affordance) that:
//   - opens a hovercard on HOVER and FOCUS (Radix, native) AND on TAP (we toggle
//     the controlled `open` so the preview is reachable without a pointer — the
//     ux-spec "hover is NEVER the only access path" rule, D-16);
//   - shows the source type-icon + title + snippet from the code-owned Source
//     registry (D-05);
//   - on click fires `onOpenSource(refId)` so the host (26-06) opens the Source
//     Explorer for that source.
//
// COLOR (UI-SPEC): a `cited` chip is accent (the one thing to click); a
// `consulted`-only chip stays neutral. Accent is otherwise scarce.

const ICON_FOR_KIND: Partial<Record<DisplayKind, string>> = {
  web_result: 'M2 12h20 M12 2a15 15 0 0 1 0 20 M12 2a15 15 0 0 0 0 20', // globe-ish
  document: 'M7 3h7l4 4v14H7z M14 3v4h4', // page
  code: 'm8 9-3 3 3 3 M16 9l3 3-3 3', // angle brackets
};

/** A small type glyph for the hovercard heading; falls back to a generic doc mark. */
function KindIcon({ kind }: { readonly kind?: DisplayKind }) {
  const path = (kind !== undefined ? ICON_FOR_KIND[kind] : undefined) ?? 'M7 3h10v18H7z';
  return (
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
      className="shrink-0 text-text-faint"
    >
      <path d={path} />
    </svg>
  );
}

export interface CitationBubbleProps {
  /** The 1-based citation number rendered in the chip. */
  readonly number: number;
  /** The resolved registry source (title/snippet/type/cited). */
  readonly source: DisplaySource;
  /** Fired on click with the source ref_id — the host opens the Source Explorer. */
  readonly onOpenSource?: (refId: string) => void;
}

export function CitationBubble({ number, source, onOpenSource }: CitationBubbleProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const title = source.title ?? t('citation.unknownTitle');

  const chipColor = source.cited
    ? 'bg-accent text-accent-text'
    : 'bg-surface-3 text-text-muted';

  return (
    <HoverCard openDelay={80} closeDelay={120} open={open} onOpenChange={setOpen}>
      <HoverCardTrigger asChild>
        <button
          type="button"
          // Tap path: a click toggles the controlled hovercard open AND opens the
          // source explorer — so a touch user both sees the preview and navigates.
          onClick={() => {
            setOpen(true);
            onOpenSource?.(source.ref_id);
          }}
          aria-label={t('citation.aria', { n: number, title })}
          className={`mx-0.5 inline-flex min-h-[1.25rem] min-w-[1.25rem] items-center justify-center rounded-[var(--radius-sm)] px-1 align-baseline text-[0.75rem] font-medium tabular-nums focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${chipColor}`}
        >
          {number}
        </button>
      </HoverCardTrigger>
      <HoverCardPortal>
        <HoverCardContent
          side="top"
          align="start"
          sideOffset={6}
          className="z-50 w-72 rounded-[var(--radius-md)] border border-border bg-surface-2 p-3 shadow-[var(--shadow-md)]"
        >
          <div className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <KindIcon {...(source.type !== undefined ? { kind: source.type } : {})} />
              <span className="truncate text-sm font-medium text-text">{title}</span>
              <span
                className={`ms-auto shrink-0 rounded-[var(--radius-pill)] px-1.5 py-0.5 text-[0.75rem] font-medium ${
                  source.cited ? 'text-accent-text' : 'text-text-faint'
                }`}
              >
                {source.cited ? t('citation.cited') : t('citation.consulted')}
              </span>
            </div>
            {source.snippet !== undefined && source.snippet.length > 0 ? (
              <p className="line-clamp-3 text-[0.75rem] leading-snug text-text-muted">
                {source.snippet}
              </p>
            ) : null}
            {source.url !== undefined && source.url.length > 0 ? (
              <span className="truncate text-[0.75rem] text-text-faint">{source.url}</span>
            ) : null}
          </div>
        </HoverCardContent>
      </HoverCardPortal>
    </HoverCard>
  );
}
