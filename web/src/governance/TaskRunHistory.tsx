import { useState } from 'react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { fetchSchedulerRuns, type SchedulerRun, type SchedulerTask } from './governanceApi';

// TaskRunHistory — the detail pane for a selected scheduler task (GOV-03): the paginated,
// newest-first run history. Pagination is page-size accumulation — each "Show more" raises the
// requested limit by RUN_PAGE, and a "Showing {{shown}} of {{total}}" status reports the count
// (total is unknown until a short page arrives, at which point shown===total). The run rows show
// status / started / heartbeat / summary as React-escaped text, mono for the timestamps. The
// close button restores focus to the originating task row (BoardLayout wiring).

const RUN_PAGE = 25;

export interface TaskRunHistoryProps {
  readonly task: SchedulerTask;
  readonly onClose: () => void;
}

export function TaskRunHistory({ task, onClose }: TaskRunHistoryProps) {
  const { t } = useTranslation();
  const [limit, setLimit] = useState(RUN_PAGE);

  const runs = useQuery({
    queryKey: ['governance', 'scheduler', 'runs', task.ID, limit],
    queryFn: () => fetchSchedulerRuns(task.ID, limit, 0),
    retry: false,
    placeholderData: keepPreviousData,
  });

  const rows: readonly SchedulerRun[] = runs.data ?? [];
  // A short page (fewer than requested) means we've reached the end → shown is the total.
  const reachedEnd = rows.length < limit;
  const total = reachedEnd ? rows.length : `${String(rows.length)}+`;

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 overflow-y-auto p-4">
      <header className="flex items-start justify-between gap-2">
        <h3 className="break-words font-display text-[20px] font-semibold text-text">
          {t('governance.scheduler.runs.heading')}
        </h3>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('governance.closeAria')}
          className="min-h-[44px] min-w-[44px] rounded-md text-text-muted hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          ✕
        </button>
      </header>

      {runs.isLoading ? (
        <p role="status" className="text-[15.5px] text-text-muted">
          {t('governance.loading')}
        </p>
      ) : runs.isError ? (
        <p role="alert" className="text-[15.5px] text-danger">
          {t('governance.error')}
        </p>
      ) : rows.length === 0 ? (
        <p className="text-[15.5px] text-text-muted">{t('governance.scheduler.runs.empty')}</p>
      ) : (
        <>
          <ul className="flex flex-col gap-2">
            {rows.map((run) => (
              <li
                key={run.ID}
                className="flex flex-col gap-1 rounded-md border border-border bg-surface-2 px-3 py-2"
              >
                <span className="flex items-center justify-between gap-2">
                  <span className="text-[13px] font-semibold text-text">
                    {t('governance.scheduler.runs.status')}: {run.Status}
                  </span>
                  <span className="shrink-0 font-mono text-[13px] tracking-tight text-text-muted">
                    {run.StartedAt}
                  </span>
                </span>
                {run.Summary !== '' ? (
                  <span className="break-words text-[13px] text-text-muted">{run.Summary}</span>
                ) : null}
                {run.LastHeartbeatAt !== '' && !run.LastHeartbeatAt.startsWith('0001-01-01') ? (
                  <span className="break-all font-mono text-[13px] tracking-tight text-text-muted">
                    {t('governance.scheduler.runs.heartbeat')}: {run.LastHeartbeatAt}
                  </span>
                ) : null}
              </li>
            ))}
          </ul>

          <div className="flex items-center justify-between gap-2">
            <span role="status" className="text-[13px] text-text-muted">
              {t('governance.pagination.status', { shown: rows.length, total })}
            </span>
            {!reachedEnd ? (
              <button
                type="button"
                onClick={() => {
                  setLimit((n) => n + RUN_PAGE);
                }}
                className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text transition-colors hover:border-border-strong focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {t('governance.pagination.more')}
              </button>
            ) : null}
          </div>
        </>
      )}
    </div>
  );
}
