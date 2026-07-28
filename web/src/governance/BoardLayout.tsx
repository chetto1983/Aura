import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { focusFirstDescendant, trapTabKey } from '../a11y/focusTrap';
import { useBoardMasterWidth } from './useBoardMasterWidth';

// BoardLayout — the master-list + detail shell shared by all three governance boards. Desktop
// (lg) is a two-column grid: the master list on the left, the detail pane on the right (showing
// the detail-empty copy when nothing is selected). Below lg the master list is dominant and the
// detail is a backdrop-dismissable BOTTOM SHEET (the GraphExplorer inspector pattern,
// GraphExplorer.tsx:327-372): `fixed inset-x-0 bottom-0 max-h-[78svh]`, a `bg-black/50 lg:hidden`
// backdrop that taps to dismiss, and a focus TRAP while open that RESTORES focus to the
// originating row on close (28-UI-SPEC §A11y: bottom sheets trap+restore focus).

export interface BoardLayoutProps {
  readonly master: ReactNode;
  /** The detail node, or undefined when no row is selected (desktop shows detail-empty copy). */
  readonly detail: ReactNode | undefined;
  /** True when a row is selected — drives the mobile sheet + backdrop. */
  readonly detailOpen: boolean;
  readonly onCloseDetail: () => void;
  /** The element to restore focus to when the mobile sheet closes (the originating row). */
  readonly restoreFocusRef: React.RefObject<HTMLElement | null>;
  readonly detailLabel: string;
}

export function BoardLayout({
  master,
  detail,
  detailOpen,
  onCloseDetail,
  restoreFocusRef,
  detailLabel,
}: BoardLayoutProps) {
  const { t } = useTranslation();
  const sheetRef = useRef<HTMLDivElement | null>(null);
  const masterWidth = useBoardMasterWidth();

  // Focus trap + restore for the MOBILE bottom sheet only (lg renders the detail as a static
  // column where trapping would be wrong). When the sheet opens on a narrow viewport, move focus
  // into it and keep Tab cycling inside; on close, return focus to the originating row.
  useEffect(() => {
    if (!detailOpen) {
      return;
    }
    // The focus trap is a MOBILE-only concern (the lg detail is a static column). Guard the
    // matchMedia lookup: a runtime without it (jsdom / older embed) simply skips the trap rather
    // than crashing the board.
    const isMobile =
      typeof window.matchMedia === 'function'
        ? window.matchMedia('(max-width: 1023px)').matches
        : false;
    if (!isMobile) {
      return;
    }
    const sheet = sheetRef.current;
    if (sheet === null) {
      return;
    }
    const previouslyFocused = restoreFocusRef.current;
    focusFirstDescendant(sheet);

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCloseDetail();
        return;
      }
      trapTabKey(event, sheet);
    }

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      previouslyFocused?.focus();
    };
  }, [detailOpen, onCloseDetail, restoreFocusRef]);

  return (
    <div
      className="relative flex h-full min-h-0 flex-col lg:grid lg:grid-cols-[var(--board-master-w)_auto_minmax(0,1fr)]"
      style={{ '--board-master-w': `${String(masterWidth.width)}px` } as React.CSSProperties}
    >
      {/* MASTER list — dominant on mobile, the left column on lg. */}
      <div className="min-h-0 flex-1 overflow-y-auto lg:col-start-1">{master}</div>

      {/* The resize handle is its own grid track so it never overlaps either column: drag it,
          or focus it and use the arrow keys (Shift for a bigger step, Home/End for the bounds).
          It is a <button> carrying role="separator" — the WAI-ARIA window-splitter pattern
          wants a FOCUSABLE separator, and starting from a natively-focusable element is what
          keeps the keyboard contract from rotting into an unreachable div. The rule below has
          no model for that pattern and reads "separator" as decorative. */}
      {/* eslint-disable jsx-a11y/no-interactive-element-to-noninteractive-role */}
      <button
        type="button"
        role="separator"
        aria-orientation="vertical"
        aria-label={t('governance.resizeList')}
        aria-valuenow={masterWidth.width}
        aria-valuemin={masterWidth.min}
        aria-valuemax={masterWidth.max}
        onPointerDown={masterWidth.onPointerDown}
        onKeyDown={masterWidth.onKeyDown}
        className="hidden lg:col-start-2 lg:block lg:w-px lg:cursor-col-resize lg:bg-border lg:outline-none lg:ring-inset hover:lg:bg-border-strong focus-visible:lg:ring-2 focus-visible:lg:ring-ring"
      />
      {/* eslint-enable jsx-a11y/no-interactive-element-to-noninteractive-role */}

      {/* DETAIL — desktop right column (detail-empty when nothing selected); mobile bottom sheet
          on selection. ONE instance (no duplicate DOM). */}
      <aside
        ref={sheetRef}
        aria-label={detailLabel}
        // The aside is the ONE scroll container for the detail on both regimes, and
        // overscroll-contain belongs here rather than on the pane inside it: on the
        // scroller it does what it is for (the page behind the sheet stays put), while on
        // a nested pane that cannot scroll it silently eats the gesture instead — a
        // container with no scrollable overflow counts as permanently at its scroll
        // boundary, so `contain` blocks chaining to the ancestor that COULD scroll
        // (Chrome 144 extended this to non-scrollable containers).
        className={`min-h-0 lg:col-start-3 lg:static lg:block lg:overflow-y-auto lg:overscroll-contain lg:bg-surface lg:[scrollbar-gutter:stable] ${
          detailOpen
            ? 'fixed inset-x-0 bottom-0 z-40 max-h-[78svh] overflow-y-auto overscroll-contain rounded-t-xl border-t border-border bg-surface shadow-2xl lg:inset-auto lg:z-auto lg:max-h-none lg:rounded-none lg:border-t-0 lg:shadow-none'
            : 'hidden lg:block'
        }`}
      >
        {detail ?? (
          <div className="grid h-full place-items-center p-8 text-center">
            <p className="text-[15.5px] text-text-muted">{t('governance.detailEmpty')}</p>
          </div>
        )}
      </aside>

      {/* Mobile sheet backdrop (lg:hidden). Tap to dismiss. */}
      {detailOpen ? (
        <button
          type="button"
          aria-label={t('governance.closeAria')}
          onClick={onCloseDetail}
          className="fixed inset-0 z-30 bg-black/50 lg:hidden"
        />
      ) : null}
    </div>
  );
}
