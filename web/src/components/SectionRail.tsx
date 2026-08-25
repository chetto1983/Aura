import { useEffect, useRef } from 'react';
import type { LucideIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ColumnResizeHandle } from './ColumnResizeHandle';
import { useColumnResize } from './useColumnResize';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

// SectionRail — the one section navigator for every workspace that splits into panes
// (Settings, Governance). It wears the CHAT sidebar's language so the three surfaces read as
// one app: an `aside`-width column on `bg-surface` behind a `border-r` from `lg` up, rows that
// are ghost Buttons with the `aria-[current=page]:bg-surface-2` selected state
// (AppShell.tsx:451-455 + shell/MobileAppSidebar.tsx:40-52 are the originals it copies).
//
// Below `lg` the same rows become a horizontally scrollable strip above the pane: a column
// there would eat the pane, and a fixed-column grid — what Governance used to do — squeezes
// every label into an ellipsis the moment a language is wordier than English.
//
// The column is drag-resizable from the same bounds as the chat rail (15rem default, 13-28rem),
// remembered per rail. That is what keeps a wide pane usable beside it: Governance's boards are
// already master + detail, so the rail is a THIRD column there and the operator has to be able
// to give it back its width.

// The chat rail's own sizes (AppShell.tsx:456-461), so the two sidebars agree at rest.
const RAIL_DEFAULT_WIDTH = 240;
const RAIL_MIN_WIDTH = 208;
const RAIL_MAX_WIDTH = 448;

export interface SectionRailItem {
  readonly id: string;
  readonly icon: LucideIcon;
  readonly label: string;
}

export interface SectionRailGroup {
  readonly id: string;
  /** Omit for a flat rail: no caption is rendered and the list carries no `aria-labelledby`. */
  readonly caption?: string;
  readonly items: readonly SectionRailItem[];
}

export interface SectionRailProps {
  /** Distinguishes this rail's remembered width from every other rail's. */
  readonly id: string;
  /** Names the nav landmark — every rail needs its own ("Settings sections"). */
  readonly label: string;
  readonly groups: readonly SectionRailGroup[];
  readonly activeId: string;
  readonly onSelect: (id: string) => void;
}

export function SectionRail({ id, label, groups, activeId, onSelect }: SectionRailProps) {
  const { t } = useTranslation();
  const activeRef = useRef<HTMLButtonElement | null>(null);
  const navRef = useRef<HTMLElement | null>(null);
  const resize = useColumnResize({
    originRef: navRef,
    storageKey: `aura.rail.${id}.width`,
    defaultWidth: RAIL_DEFAULT_WIDTH,
    min: RAIL_MIN_WIDTH,
    max: RAIL_MAX_WIDTH,
  });
  // On the narrow strip the remembered pane can sit past the right edge, so the rail would
  // open showing a selection the operator cannot see. Scrolling it into view is a no-op in
  // the desktop column, where every item is already on screen.
  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [activeId]);

  return (
    <>
      <nav
        ref={navRef}
        aria-label={label}
        // The width track is inert below `lg`, where the rail is a full-width strip — that is
        // what lets one tree serve both regimes instead of swapping (and remounting) at the
        // breakpoint.
        style={{ '--rail-w': `${String(resize.width)}px` } as React.CSSProperties}
        className={cn(
          'flex shrink-0 gap-1 overflow-x-auto border-b border-border bg-surface px-3 py-2',
          'lg:w-[var(--rail-w)] lg:flex-col lg:gap-3 lg:overflow-x-visible lg:overflow-y-auto lg:border-r lg:border-b-0 lg:py-3',
        )}
      >
        {groups
          .filter((group) => group.items.length > 0)
          .map((group) => (
            <SectionRailGroupList
              key={group.id}
              group={group}
              activeId={activeId}
              activeRef={activeRef}
              onSelect={onSelect}
            />
          ))}
      </nav>
      <ColumnResizeHandle resize={resize} label={t('shell.resizeSections')} />
    </>
  );
}

function SectionRailGroupList({
  group,
  activeId,
  activeRef,
  onSelect,
}: {
  readonly group: SectionRailGroup;
  readonly activeId: string;
  readonly activeRef: React.RefObject<HTMLButtonElement | null>;
  readonly onSelect: (id: string) => void;
}) {
  const captionId = `section-rail-${group.id}`;
  const caption = group.caption;

  return (
    <div className="flex gap-1 lg:flex-col lg:gap-1">
      {/* The caption stays `sr-only` on the strip rather than `hidden`, so it keeps labelling
          its list for a screen reader at every width. */}
      {caption === undefined ? null : (
        <p
          id={captionId}
          className="max-lg:sr-only px-2 text-[0.8125rem] font-semibold text-text-faint"
        >
          {caption}
        </p>
      )}
      <ul
        aria-labelledby={caption === undefined ? undefined : captionId}
        className="flex gap-1 lg:flex-col lg:gap-0.5"
      >
        {group.items.map((item) => {
          const Icon = item.icon;
          const isActive = item.id === activeId;
          return (
            <li key={item.id}>
              <Button
                type="button"
                variant="ghost"
                ref={isActive ? activeRef : undefined}
                aria-current={isActive ? 'page' : undefined}
                onClick={() => {
                  onSelect(item.id);
                }}
                className={cn(
                  // No height of its own: the Button's `min-h-[44px]` is the touch floor in
                  // REAL pixels. An `h-11` here would look identical and measure 42.6px,
                  // because Aura's root font is not 16px.
                  'justify-start rounded-md px-2 text-[14px] font-medium lg:w-full',
                  'text-text-muted hover:bg-surface-2 hover:text-text',
                  'aria-[current=page]:bg-surface-2 aria-[current=page]:text-text',
                )}
              >
                <Icon data-icon="inline-start" aria-hidden="true" focusable="false" />
                <span className="min-w-0 truncate">{item.label}</span>
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
