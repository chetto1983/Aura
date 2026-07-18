import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { BoardLayout } from './BoardLayout';
import { BoardStateView, boardStatus } from './governanceView';
import { TaskRunHistory } from './TaskRunHistory';
import { SchedulerEditDialog } from './SchedulerEditDialog';
import {
  SCHEDULER_QUERY_KEY,
  useApproveTask,
  useCancelTask,
  useRunTask,
} from './useSchedulerMutations';
import { fetchSchedulerTasks, isUserManageableKind, type SchedulerTask } from './governanceApi';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// SchedulerBoard (GOV-03) is the mission-control scheduler: a dense, monospace task strip
// where each row is a channel — a status LED (green=active, amber pulsing=awaiting approval),
// the kind, the schedule + next fire as first-class monospace literals, and the operator
// verbs (approve / run / edit / delete) inline. System-seeded sweeps are read-only (a SYSTEM
// tag, no verbs). Selecting a row opens its run history. The signature is the LED status
// system + the tabular next-fire; everything else stays quiet.

// scheduleText renders the schedule as a monospace literal: the cron string, the every-N
// interval, or the one-shot instant — falling back to a dash for an unschedulable row.
// eslint-disable-next-line react-refresh/only-export-components
export function scheduleText(task: SchedulerTask, dash: string): string {
  if (task.CronExpr !== '') return task.CronExpr;
  if (task.EveryMinutes > 0) return `every ${String(task.EveryMinutes)}m`;
  if (task.RunAt !== '' && !task.RunAt.startsWith('0001-01-01')) return task.RunAt;
  return dash;
}

// formatFire trims the RFC-3339 next-fire to a compact "YYYY-MM-DD HH:MM:SSZ" and guards the
// zero-time (0001-…) an unschedulable task carries — the raw value the old board leaked.
function formatFire(raw: string, dash: string): string {
  if (raw === '' || raw.startsWith('0001')) return dash;
  const m = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})/.exec(raw);
  const date = m?.[1];
  const time = m?.[2];
  return date !== undefined && time !== undefined ? `${date} ${time}Z` : raw;
}

// ledClass + chipClass map a status to its control-panel color: active is a steady green,
// awaiting-approval an amber that pulses for attention, anything else a muted dot.
function ledClass(status: string): string {
  if (status === 'active') return 'bg-success';
  if (status === 'pending_approval') return 'bg-warning animate-pulse';
  return 'bg-text-muted';
}

function chipClass(status: string): string {
  if (status === 'active') return 'text-success border-success/40';
  if (status === 'pending_approval') return 'text-warning border-warning/40';
  return 'text-text-muted border-border';
}

export function SchedulerBoard() {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const [editing, setEditing] = useState<SchedulerTask | null>(null);
  const [pendingDelete, setPendingDelete] = useState<SchedulerTask | null>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const dash = t('governance.scheduler.field.none');

  const approve = useApproveTask();
  const run = useRunTask();
  const cancel = useCancelTask();

  const tasks = useQuery({
    queryKey: SCHEDULER_QUERY_KEY,
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
    <div className="flex flex-col">
      <header className="flex items-center justify-between border-b border-border px-3 py-2">
        <span className="font-mono text-[13px] font-semibold uppercase tracking-[0.18em] text-text">
          {t('governance.tabs.scheduler')}
        </span>
        <span className="font-mono text-[12px] tabular-nums text-text-faint">
          {t('governance.scheduler.countLabel', { count: rows.length })}
        </span>
      </header>

      <ul aria-label={t('governance.tabs.scheduler')} className="flex flex-col">
        {rows.map((task) => {
          const isSelected = selected === task.ID;
          const manageable = isUserManageableKind(task.Kind);
          return (
            <li
              key={task.ID}
              className={`group grid grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-border/60 px-3 py-2 transition-colors ${
                isSelected ? 'bg-surface-3' : 'hover:bg-surface-2'
              }`}
            >
              {/* Status LED rail */}
              <span
                aria-hidden
                className={`h-2.5 w-2.5 shrink-0 rounded-full ${ledClass(task.Status)}`}
              />

              {/* Selectable channel body → opens the run history */}
              <button
                type="button"
                aria-pressed={isSelected}
                onClick={(e) => {
                  selectRow(task.ID, e.currentTarget);
                }}
                className="flex min-w-0 flex-col items-start gap-0.5 text-left"
              >
                <span className="flex items-center gap-2">
                  <span className="text-[15px] font-semibold text-text">{task.Kind}</span>
                  <span
                    className={`rounded-sm border px-1.5 py-px font-mono text-[10px] uppercase tracking-wider ${chipClass(task.Status)}`}
                  >
                    {task.Status === 'pending_approval'
                      ? t('governance.scheduler.awaiting')
                      : task.Status}
                  </span>
                  {!manageable && (
                    <span className="rounded-sm border border-border px-1.5 py-px font-mono text-[10px] uppercase tracking-wider text-text-faint">
                      {t('governance.scheduler.system')}
                    </span>
                  )}
                </span>
                <span className="flex min-w-0 items-center gap-2 font-mono text-[12px] text-text-muted">
                  <span className="truncate">{scheduleText(task, dash)}</span>
                  <span aria-hidden className="text-text-disabled">
                    ·
                  </span>
                  <span className="shrink-0 tabular-nums text-text-faint">
                    {formatFire(task.NextRunAt, dash)}
                  </span>
                </span>
              </button>

              {/* Operator verbs — only for user-managed kinds */}
              {manageable && (
                <div className="flex items-center gap-1">
                  {task.Status === 'pending_approval' && (
                    <Button
                      type="button"
                      size="sm"
                      variant="default"
                      disabled={approve.isPending}
                      onClick={() => {
                        approve.mutate(task.ID);
                      }}
                    >
                      {approve.isPending
                        ? t('governance.scheduler.actions.approving')
                        : t('governance.scheduler.actions.approve')}
                    </Button>
                  )}
                  {task.Status === 'active' && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={run.isPending}
                      onClick={() => {
                        run.mutate(task.ID);
                      }}
                    >
                      {run.isPending
                        ? t('governance.scheduler.actions.running')
                        : t('governance.scheduler.actions.run')}
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setEditing(task);
                    }}
                  >
                    {t('governance.scheduler.actions.edit')}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    className="text-danger hover:text-danger"
                    onClick={() => {
                      setPendingDelete(task);
                    }}
                  >
                    {t('governance.scheduler.actions.delete')}
                  </Button>
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );

  return (
    <>
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

      {editing !== null && (
        <SchedulerEditDialog
          task={editing}
          open
          onClose={() => {
            setEditing(null);
          }}
        />
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(next) => {
          if (!next) setPendingDelete(null);
        }}
        title={t('governance.scheduler.delete.title')}
        description={t('governance.scheduler.delete.body', { kind: pendingDelete?.Kind ?? '' })}
        cancelLabel={t('governance.scheduler.delete.cancel')}
        confirmLabel={t('governance.scheduler.delete.confirm')}
        confirmVariant="destructive"
        onConfirm={() => {
          const target = pendingDelete;
          setPendingDelete(null);
          if (target) cancel.mutate(target.ID);
        }}
      />
    </>
  );
}
