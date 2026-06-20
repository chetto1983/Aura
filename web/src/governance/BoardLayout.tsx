import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

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

  // Focus trap + restore for the MOBILE bottom sheet only (lg renders the detail as a static
  // column where trapping would be wrong). When the sheet opens on a narrow viewport, move focus
  // into it and keep Tab cycling inside; on close, return focus to the originating row.
  useEffect(() => {
    if (!detailOpen) {
      return;
    }
    const isMobile = window.matchMedia('(max-width: 1023px)').matches;
    if (!isMobile) {
      return;
    }
    const sheet = sheetRef.current;
    if (sheet === null) {
      return;
    }
    const previouslyFocused = restoreFocusRef.current;
    const focusables = (): HTMLElement[] =>
      Array.from(
        sheet.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
        ),
      ).filter((el) => !el.hasAttribute('disabled'));

    focusables()[0]?.focus();

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCloseDetail();
        return;
      }
      if (event.key !== 'Tab') {
        return;
      }
      const items = focusables();
      if (items.length === 0) {
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      if (first === undefined || last === undefined) {
        return;
      }
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      previouslyFocused?.focus();
    };
  }, [detailOpen, onCloseDetail, restoreFocusRef]);

  return (
    <div className="relative flex h-full min-h-0 flex-col lg:grid lg:grid-cols-[20rem_minmax(0,1fr)]">
      {/* MASTER list — dominant on mobile, the left column on lg. */}
      <div className="min-h-0 flex-1 overflow-y-auto lg:col-start-1 lg:border-r lg:border-border">
        {master}
      </div>

      {/* DETAIL — desktop right column (detail-empty when nothing selected); mobile bottom sheet
          on selection. ONE instance (no duplicate DOM). */}
      <aside
        ref={sheetRef}
        aria-label={detailLabel}
        className={`min-h-0 lg:col-start-2 lg:static lg:block lg:overflow-y-auto lg:bg-surface ${
          detailOpen
            ? 'fixed inset-x-0 bottom-0 z-40 max-h-[78svh] overflow-y-auto rounded-t-xl border-t border-border bg-surface shadow-2xl lg:inset-auto lg:z-auto lg:max-h-none lg:rounded-none lg:border-t-0 lg:shadow-none'
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
