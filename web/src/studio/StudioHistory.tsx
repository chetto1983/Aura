import { useEffect, useState } from 'react';
import { PanelRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Drawer } from '../shell/Drawer';
import { StudioHistoryCard } from './StudioHistoryCard';
import type { StudioRecord } from './studioApi';
import { Input } from '@/components/ui/input';
import { Toggle } from '@/components/ui/toggle';

// StudioHistory — the identity's own generations, newest first. Wide enough and it is a panel
// beside the page; narrower and it is the cockpit's own Drawer over it, which is what brings
// the dialog semantics, the focus trap, Escape, the backdrop and the scroll lock. Rolling
// those by hand left keyboard focus walking through a composer nobody could see.
//
// It remembers whether it is open, because an operator who closed it to see a 4K frame should
// not find it back on the next reload.

const OPEN_KEY = 'aura.studio.history-open';

/** `lg`, the width the rest of the cockpit switches its side panels at, and the width the
 *  shared Drawer hides itself above (`lg:hidden`). One breakpoint, so the two cannot
 *  disagree about which of them is on screen. */
const SIDE_BY_SIDE = '(min-width: 64rem)';

function isSideBySide(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true;
  return window.matchMedia(SIDE_BY_SIDE).matches;
}

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
  return isSideBySide();
}

/** Whether there is room beside the page. The width is read once for the first render and
 *  then only from the listener, so nothing calls setState while an effect is synchronising. */
function useSideBySide(): boolean {
  const [wide, setWide] = useState(isSideBySide);
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia(SIDE_BY_SIDE);
    const onChange = () => {
      setWide(query.matches);
    };
    query.addEventListener('change', onChange);
    return () => {
      query.removeEventListener('change', onChange);
    };
  }, []);
  return wide;
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

export function StudioHistory(props: StudioHistoryProps) {
  const { t } = useTranslation();
  const sideBySide = useSideBySide();
  const [open, setOpen] = useState(readOpen);

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

  const toggle = (
    <Toggle
      size="sm"
      pressed={open}
      onPressedChange={show}
      aria-label={open ? t('studio.history.hide') : t('studio.history.show')}
      className="text-text-faint hover:text-text"
    >
      <PanelRight aria-hidden="true" />
    </Toggle>
  );

  if (!sideBySide) {
    return (
      <>
        <div className="absolute top-1 right-1 z-20 rounded-[var(--radius-md)] bg-bg/80 p-1 backdrop-blur">
          {toggle}
        </div>
        <Drawer
          open={open}
          title={t('studio.history.title')}
          side="right"
          onClose={() => {
            show(false);
          }}
        >
          <div className="flex h-full flex-col gap-2 p-2">
            <HistoryBody {...props} />
          </div>
        </Drawer>
      </>
    );
  }

  return (
    <aside
      aria-label={t('studio.history.title')}
      className={
        open
          ? 'flex w-72 shrink-0 flex-col gap-2 overflow-hidden border-l border-border bg-surface p-2'
          : 'flex shrink-0 flex-col border-l border-border bg-surface p-1'
      }
    >
      <div className="flex items-center justify-between gap-2">
        {open ? (
          <h2 className="ps-1 text-[13px] font-semibold text-text">{t('studio.history.title')}</h2>
        ) : null}
        {toggle}
      </div>
      {open ? <HistoryBody {...props} /> : null}
    </aside>
  );
}

/** The search box, the list and its three not-a-list states. Shared by the panel and the
 *  drawer so neither can drift into saying something the other does not. */
function HistoryBody({
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
  const [query, setQuery] = useState('');
  const needle = query.trim().toLowerCase();
  const shown =
    needle === ''
      ? records
      : records.filter((record) => record.prompt.toLowerCase().includes(needle));

  return (
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
  );
}
