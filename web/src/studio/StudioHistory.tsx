import { useEffect, useState } from 'react';
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

function readOpen(): boolean {
  try {
    // Open is the default: the panel is where a submitted clip goes, and a first visit that
    // hides it makes a running job look like nothing happened.
    return localStorage.getItem(OPEN_KEY) !== '0';
  } catch {
    return true;
  }
}

interface StudioHistoryProps {
  readonly records: readonly StudioRecord[];
  readonly selectedId: string | undefined;
  readonly hasMore: boolean;
  readonly loadingMore: boolean;
  readonly onSelect: (record: StudioRecord) => void;
  readonly onLoadMore: () => void;
}

export function StudioHistory({
  records,
  selectedId,
  hasMore,
  loadingMore,
  onSelect,
  onLoadMore,
}: StudioHistoryProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(readOpen);
  const [query, setQuery] = useState('');

  useEffect(() => {
    try {
      localStorage.setItem(OPEN_KEY, open ? '1' : '0');
    } catch {
      // Persistence is best-effort; the panel still obeys the toggle for this session.
    }
  }, [open]);

  const needle = query.trim().toLowerCase();
  const shown =
    needle === ''
      ? records
      : records.filter((record) => record.prompt.toLowerCase().includes(needle));

  return (
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
          <h2 className="ps-1 text-[13px] font-semibold text-text">{t('studio.history.title')}</h2>
        ) : null}
        <Toggle
          size="sm"
          pressed={open}
          onPressedChange={setOpen}
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

          {shown.length === 0 ? (
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
  );
}
