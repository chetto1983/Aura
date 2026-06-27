import { useState } from 'react';
import { Code2, File, FileText, Globe, type LucideIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  HoverCard,
  HoverCardContent,
  HoverCardPortal,
  HoverCardTrigger,
} from '@radix-ui/react-hover-card';
import type { DisplayKind, DisplaySource } from './types';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

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

const ICON_FOR_KIND: Partial<Record<DisplayKind, LucideIcon>> = {
  web_result: Globe,
  document: FileText,
  code: Code2,
};

/** A small type glyph for the hovercard heading; falls back to a generic doc mark. */
function KindIcon({ kind }: { readonly kind?: DisplayKind }) {
  const Icon = (kind !== undefined ? ICON_FOR_KIND[kind] : undefined) ?? File;
  return <Icon data-icon aria-hidden="true" className="shrink-0 text-text-faint" />;
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

  return (
    <HoverCard openDelay={80} closeDelay={120} open={open} onOpenChange={setOpen}>
      <HoverCardTrigger asChild>
        <Button
          type="button"
          variant={source.cited ? 'default' : 'secondary'}
          // Tap path: a click toggles the controlled hovercard open AND opens the
          // source explorer — so a touch user both sees the preview and navigates.
          onClick={() => {
            setOpen(true);
            onOpenSource?.(source.ref_id);
          }}
          aria-label={t('citation.aria', { n: number, title })}
          className="mx-0.5 inline-flex h-auto min-h-[1.25rem] min-w-[1.25rem] rounded-[var(--radius-sm)] px-1 py-0 align-baseline text-[0.75rem] tabular-nums"
        >
          {number}
        </Button>
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
              <Badge
                variant={source.cited ? 'default' : 'secondary'}
                className="ms-auto text-[0.75rem]"
              >
                {source.cited ? t('citation.cited') : t('citation.consulted')}
              </Badge>
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
