import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { BoardLayout } from './BoardLayout';
import { BoardStateView, boardStatus } from './governanceView';
import { TaskRunHistory } from './TaskRunHistory';
import { fetchSchedulerTasks, type SchedulerTask } from './governanceApi';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

// SchedulerBoard (GOV-03) — the scheduled-tasks master list + paginated run-history detail. The
// task list comes from fetchSchedulerTasks (read-only — mutates nothing). Each row shows
// kind / schedule / next-run / status, with the cron expression in mono (it is a literal). The
// schedule cell prefers the cron string, falling back to the every-N-minutes / one-shot RunAt
// shape. Selecting a row opens TaskRunHistory (the paginated, newest-first runs with the
// Show-more / "Showing X of Y" control) as a desktop column / mobile bottom sheet.

// eslint-disable-next-line react-refresh/only-export-components
export function scheduleText(task: SchedulerTask, dash: string): string {
  if (task.CronExpr !== '') {
    return task.CronExpr;
  }
  if (task.EveryMinutes > 0) {
    return `every ${String(task.EveryMinutes)}m`;
  }
  if (task.RunAt !== '' && !task.RunAt.startsWith('0001-01-01')) {
    return task.RunAt;
  }
  return dash;
}

export function SchedulerBoard() {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const dash = t('governance.scheduler.field.none');

  const tasks = useQuery({
    queryKey: ['governance', 'scheduler', 'tasks'],
    queryFn: fetchSchedulerTasks,
    retry: false,
  });

  const rows: readonly SchedulerTask[] = tasks.data ?? [];
  const status = boardStatus({
    isLoading: tasks.isLoading,
    isError: tasks.isError,
    error: tasks.error,
    isEmpty: rows.length === 0,
  });

  const selectedTask = rows.find((task) => task.ID === selected);

  function selectRow(id: string, el: HTMLElement | null) {
    restoreFocusRef.current = el;
    setSelected(id);
  }

  const master = (
    <div
      role="list"
      aria-label={t('governance.tabs.scheduler')}
      className="flex flex-col gap-1 p-2"
    >
      {rows.map((task) => (
        <div key={task.ID} role="listitem">
          <Button
            type="button"
            variant={selected === task.ID ? 'default' : 'outline'}
            aria-pressed={selected === task.ID}
            onClick={(e) => {
              selectRow(task.ID, e.currentTarget);
            }}
            className={`h-auto min-h-[44px] w-full flex-col items-stretch justify-start gap-1 px-3 py-2 text-left ${
              selected === task.ID
                ? 'text-on-accent'
                : 'bg-surface-2 text-text hover:border-border-strong'
            }`}
          >
            <span className="flex items-center justify-between gap-2">
              <span className="break-words text-[15.5px] font-semibold">{task.Kind}</span>
              <Badge
                variant="secondary"
                className={
                  selected === task.ID
                    ? 'border-on-accent/30 bg-on-accent/15 text-on-accent'
                    : undefined
                }
              >
                {task.Status}
              </Badge>
            </span>
            <span className="flex items-center justify-between gap-2 text-[13px] text-text-muted">
              {/* Cron / schedule string — mono (it is a literal the operator could mistake). */}
              <span className="break-all font-mono">{scheduleText(task, dash)}</span>
              <span className="shrink-0 font-mono text-[13px] tabular-nums tracking-tight">
                {task.NextRunAt}
              </span>
            </span>
          </Button>
        </div>
      ))}
    </div>
  );

  return (
    <BoardStateView
      status={status}
      emptyHeading={t('governance.scheduler.emptyHeading')}
      emptyBody={t('governance.scheduler.emptyBody')}
      onRetry={() => {
        void tasks.refetch();
      }}
    >
      <BoardLayout
        master={master}
        detail={
          selectedTask !== undefined ? (
            <TaskRunHistory
              task={selectedTask}
              onClose={() => {
                setSelected(undefined);
              }}
            />
          ) : undefined
        }
        detailOpen={selectedTask !== undefined}
        onCloseDetail={() => {
          setSelected(undefined);
        }}
        restoreFocusRef={restoreFocusRef}
        detailLabel={t('governance.detailEmpty')}
      />
    </BoardStateView>
  );
}
