import { useState } from 'react';
import { PanelRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { StudioHistoryCard } from './StudioHistoryCard';
import type { StudioRecord } from './studioApi';
import { Input } from '@/components/ui/input';
import { Toggle } from '@/components/ui/toggle';

// StudioHistory — the identity's own generations, newest first. It is a panel on a desktop
// and a drawer over the page below `md`, and it remembers whether it is open, because an
// operator who closed it to see a 4K frame should not find it back on the next reload.

const OPEN_KEY = 'aura.studio.history-open';

/** The `md` breakpoint the panel's own classes switch on: above it there is room beside the
 *  page, below it the panel is a drawer over the composer. */
const SIDE_BY_SIDE_PX = 768;

function readOpen(): boolean {
  try {
    const stored = localStorage.getItem(OPEN_KEY);
    if (stored !== null) return stored === '1';
  } catch {
    // Storage is refused; fall through to the width default.
  }
  // With no choice on record: open where it sits beside the page — it is where a submitted
  // clip goes, and hiding it makes a running job look like nothing happened — and closed
  // where it would open over the composer the operator came here to use.
  return typeof window === 'undefined' || window.innerWidth >= SIDE_BY_SIDE_PX;
}

interface StudioHistoryProps {
  readonly records: readonly StudioRecord[];
  /** The list has not answered yet. "Nothing generated yet" would be a claim about the
   *  identity's history that nobody has checked. */
  readonly pending: boolean;
  /** Why the list could not be read, when it could not. A refused read that renders as an
   *  empty panel is an error passing silently. */
  readonly failure: string | undefined;
  readonly selectedId: string | undefined;
  readonly hasMore: boolean;
  readonly loadingMore: boolean;
  readonly onSelect: (record: StudioRecord) => void;
  readonly onLoadMore: () => void;
}

export function StudioHistory({
  records,
  pending,
  failure,
  selectedId,
  hasMore,
  loadingMore,
  onSelect,
  onLoadMore,
}: StudioHistoryProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(readOpen);
  const [query, setQuery] = useState('');

  // Written on the toggle rather than on mount, so a phone's default-closed is never stored
  // as a choice this viewer made and carried back to their desktop.
  function show(next: boolean) {
    setOpen(next);
    try {
      localStorage.setItem(OPEN_KEY, next ? '1' : '0');
    } catch {
      // Persistence is best-effort; the panel still obeys the toggle for this session.
    }
  }

  const needle = query.trim().toLowerCase();
  const shown =
    needle === ''
      ? records
      : records.filter((record) => record.prompt.toLowerCase().includes(needle));

  return (
    <>
      {/* Only below `md`, where the panel is a drawer over the page: a tap outside closes it
          rather than landing on a composer the operator cannot see. */}
      {open ? (
        <button
          type="button"
          tabIndex={-1}
          aria-hidden="true"
          onClick={() => {
            show(false);
          }}
          className="absolute inset-0 z-20 cursor-default bg-bg/60 backdrop-blur-[2px] md:hidden"
        />
      ) : null}
      <aside
        aria-label={t('studio.history.title')}
        className={
          open
            ? 'flex w-72 shrink-0 flex-col gap-2 overflow-hidden border-l border-border bg-surface p-2 max-md:absolute max-md:inset-y-0 max-md:right-0 max-md:z-30 max-md:w-[80vw] max-md:max-w-72 max-md:shadow-[var(--shadow-drawer)]'
            : 'flex shrink-0 flex-col border-l border-border bg-surface p-1'
        }
      >
        <div className="flex items-center justify-between gap-2">
          {open ? (
            <h2 className="ps-1 text-[13px] font-semibold text-text">
              {t('studio.history.title')}
            </h2>
          ) : null}
          <Toggle
            size="sm"
            pressed={open}
            onPressedChange={show}
            aria-label={open ? t('studio.history.hide') : t('studio.history.show')}
            className="text-text-faint hover:text-text"
          >
            <PanelRight aria-hidden="true" />
          </Toggle>
        </div>

        {!open ? null : (
          <>
            <Input
              type="search"
              value={query}
              aria-label={t('studio.history.searchLabel')}
              placeholder={t('studio.history.search')}
              className="min-h-9 py-1 text-xs"
              onChange={(event) => {
                setQuery(event.target.value);
              }}
            />

            {failure !== undefined ? (
              <p role="alert" className="px-1 py-6 text-center text-xs text-danger">
                {failure}
              </p>
            ) : pending ? (
              <p role="status" className="px-1 py-6 text-center text-xs text-text-muted">
                {t('studio.history.loading')}
              </p>
            ) : shown.length === 0 ? (
              <p className="px-1 py-6 text-center text-xs text-text-muted">
                {records.length === 0 ? t('studio.history.empty') : t('studio.history.emptySearch')}
              </p>
            ) : (
              <ul className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto">
                {shown.map((record, index) => (
                  <li key={record.id}>
                    <StudioHistoryCard
                      record={record}
                      index={index}
                      selected={record.id === selectedId}
                      onSelect={onSelect}
                    />
                  </li>
                ))}
              </ul>
            )}

            {hasMore ? (
              <button
                type="button"
                onClick={onLoadMore}
                disabled={loadingMore}
                className="rounded-[var(--radius-sm)] border border-border py-1.5 text-xs text-text-muted hover:bg-surface-2 hover:text-text focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:opacity-60"
              >
                {t('studio.history.more')}
              </button>
            ) : null}
          </>
        )}
      </aside>
    </>
  );
}
