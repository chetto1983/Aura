import { useEffect, useId, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import {
  CircleCheck,
  CircleX,
  Clock,
  LoaderCircle,
  MailX,
  MessageCircleQuestion,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react';
import { useElapsed } from '../durationFormat';
import { cap } from '../toolSummary';
import { useWatchWorker } from '../workers/workerWatchControls';
import type { WorkerStatus } from '../workers/workerStream';
import type { DisplayChildReport } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import {
  hasField,
  hasOptions,
  isTerminalSwarmStatus,
  statusDotClass,
  statusIconName,
  statusLabelKey,
  type SwarmStatusIconName,
} from './swarmRow';
import { Button } from '@/components/ui/button';

// SwarmReportTable (SWARM-01 / D-08): a summary table over the swarm []ChildReport
// payload — one row per worker (# goal-index / Worker child-id / Status dot+label /
// Summary). Clicking a row expands its summary + error (+ question/options for a
// needs_user_input child) IN PLACE. Status comes from the Status enum ONLY (ok /
// failed / needs_user_input), never the free-form Error text, and is conveyed by
// dot + icon + text (color is never the only signal). There is deliberately NO
// inter-agent chat / mailbox affordance (D-08); the full per-child .jsonl transcript
// drill-down is a deferred follow-up. The status/field-presence logic lives in
// swarmRow.ts (unit/mutation-tested); this file is rendering only.

export interface SwarmReportTableProps {
  readonly payload: { readonly swarm?: readonly DisplayChildReport[] };
}

const EMPTY_REPORTS: readonly DisplayChildReport[] = [];

export function SwarmReportTable({ payload }: SwarmReportTableProps) {
  const { t } = useTranslation();
  const { registerWorkers, statuses, viewReport, watchWorker } = useWatchWorker();
  const [open, setOpen] = useState<number | null>(null);
  const registrationId = useId();
  const reports = payload.swarm ?? EMPTY_REPORTS;
  const label = t('display.type.swarm_report');

  useEffect(() => {
    return registerWorkers(registrationId, reports);
  }, [registerWorkers, registrationId, reports]);

  if (reports.length === 0) {
    return (
      <DisplayCardShell label={label}>
        <div className="flex flex-col items-center gap-1 py-8 text-center">
          <p className="text-sm font-medium text-text">{t('swarm.emptyHeading')}</p>
          <p className="text-[0.75rem] text-text-faint">{t('swarm.emptyBody')}</p>
        </div>
      </DisplayCardShell>
    );
  }

  return (
    <DisplayCardShell label={label} meta={t('display.table.rowCount', { count: reports.length })}>
      <div className="overflow-x-auto rounded-[var(--radius-md)] border border-border">
        <table className="min-w-[64rem] border-collapse text-left text-sm">
          <thead>
            <tr>
              {[
                t('swarm.columns.index'),
                t('swarm.columns.worker'),
                t('swarm.columns.status'),
                t('swarm.columns.goal'),
                t('swarm.columns.duration'),
                t('swarm.columns.summary'),
              ].map((col) => (
                <th
                  key={col}
                  className="border-b border-border bg-surface-2 px-3 py-2 text-[0.75rem] font-medium uppercase text-text-faint"
                >
                  {col}
                </th>
              ))}
              <th aria-label="Actions" className="border-b border-border bg-surface-2 px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {reports.map((r, i) => {
              const expanded = open === i;
              const liveStatus = statuses.get(r.child_id);
              return (
                <SwarmRow
                  key={`${String(r.goal_index)}-${r.child_id}`}
                  report={r}
                  status={liveStatus?.status ?? r.status}
                  liveStatus={liveStatus}
                  expanded={expanded}
                  onToggle={() => {
                    setOpen(expanded ? null : i);
                  }}
                  onWatch={() => {
                    watchWorker(r.child_id, reports);
                  }}
                  onViewReport={viewReport}
                  t={t}
                />
              );
            })}
          </tbody>
        </table>
      </div>
    </DisplayCardShell>
  );
}

interface SwarmRowProps {
  readonly report: DisplayChildReport;
  readonly status: string;
  readonly liveStatus: WorkerStatus | undefined;
  readonly expanded: boolean;
  readonly onToggle: () => void;
  readonly onWatch: () => void;
  readonly onViewReport: () => void;
  readonly t: TFunction;
}

function SwarmRow({
  report,
  status,
  liveStatus,
  expanded,
  onToggle,
  onWatch,
  onViewReport,
  t,
}: SwarmRowProps) {
  const dotClass = statusDotClass(status);
  const statusLabel = t(statusLabelKey(status));
  const goal = report.goal ?? '-';
  const summary =
    (status === 'failed' || status === 'dead_letter') && hasField(report.error)
      ? report.error
      : (report.summary ?? t('swarm.noSummary'));

  return (
    <>
      <tr>
        <td colSpan={7} className="border-b border-border p-0">
          <div className="grid min-h-[var(--row-h)] grid-cols-[minmax(0,1fr)_auto] items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              onClick={onToggle}
              aria-expanded={expanded}
              aria-label={t('swarm.expand')}
              className="grid h-auto min-h-[var(--row-h)] w-full grid-cols-[2.5rem_minmax(6rem,0.8fr)_minmax(8rem,1fr)_minmax(10rem,1.4fr)_5rem_minmax(10rem,1.5fr)] justify-normal gap-2 rounded-none px-3 py-2 text-left hover:bg-surface"
            >
              <span className="font-mono text-[0.75rem] tabular-nums text-text-faint">
                {report.goal_index}
              </span>
              <span className="truncate font-mono text-xs text-text-muted">{report.child_id}</span>
              <span className="flex items-center gap-2">
                <span
                  aria-hidden="true"
                  className={`inline-block h-2 w-2 shrink-0 rounded-sm ${dotClass}`}
                />
                <StatusIcon status={status} />
                <span className="text-[0.75rem] font-medium text-text">{statusLabel}</span>
              </span>
              <span className="truncate text-sm text-text-muted" title={goal}>
                {cap(goal, 80)}
              </span>
              <WorkerDuration status={status} liveStatus={liveStatus} />
              <span className="truncate text-sm text-text-muted" title={summary}>
                {cap(summary, 300)}
              </span>
            </Button>
            <div className="flex items-center">
              <Button
                type="button"
                variant="ghost"
                onClick={onWatch}
                data-required-touch-target
                className="min-h-[44px] min-w-[44px] rounded-none px-3 text-accent-text focus-visible:ring-2 focus-visible:ring-accent"
              >
                {t('swarm.watch')}
              </Button>
              {isTerminalSwarmStatus(status) ? (
                <Button
                  type="button"
                  variant="ghost"
                  onClick={onViewReport}
                  data-required-touch-target
                  className="min-h-[44px] min-w-[44px] rounded-none px-3 text-text-muted focus-visible:ring-2 focus-visible:ring-accent"
                >
                  {t('swarm.viewReport')}
                </Button>
              ) : null}
            </div>
          </div>
        </td>
      </tr>
      {expanded ? (
        <tr>
          <td colSpan={7} className="border-b border-border bg-surface px-3 py-2">
            <dl className="flex flex-col gap-2 text-sm">
              <Field label={t('swarm.columns.goal')} value={goal} />
              <Field
                label={t('swarm.summaryLabel')}
                value={report.summary ?? t('swarm.noSummary')}
              />
              {hasField(report.error) ? (
                <Field label={t('swarm.errorLabel')} value={report.error} tone="danger" />
              ) : null}
              {hasField(report.question) ? (
                <Field label={t('swarm.questionLabel')} value={report.question} />
              ) : null}
              {hasOptions(report.options) ? (
                <div className="flex flex-col gap-1">
                  <dt className="text-[0.75rem] font-medium uppercase text-text-faint">
                    {t('swarm.optionsLabel')}
                  </dt>
                  <dd>
                    <ul className="list-inside list-disc text-text-muted">
                      {report.options.map((opt, oi) => (
                        <li key={oi}>{opt}</li>
                      ))}
                    </ul>
                  </dd>
                </div>
              ) : null}
            </dl>
          </td>
        </tr>
      ) : null}
    </>
  );
}

const STATUS_ICONS: Record<SwarmStatusIconName, LucideIcon> = {
  CircleCheck,
  CircleX,
  MessageCircleQuestion,
  LoaderCircle,
  Clock,
  MailX,
  TriangleAlert,
};

function StatusIcon({ status }: { readonly status: string }) {
  const name = statusIconName(status);
  const Icon = STATUS_ICONS[name];
  return (
    <Icon
      aria-hidden="true"
      data-worker-status-icon={name}
      className={`size-4 shrink-0 ${status === 'running' ? 'animate-spin' : ''}`}
    />
  );
}

function WorkerDuration({
  status,
  liveStatus,
}: {
  readonly status: string;
  readonly liveStatus: WorkerStatus | undefined;
}) {
  const [mountedAt] = useState(() => Date.now());
  const parsedLastEvent =
    liveStatus === undefined ? Number.NaN : Date.parse(liveStatus.last_event_at);
  const endAt = Number.isFinite(parsedLastEvent) ? parsedLastEvent : mountedAt;
  const startedAt = endAt - (liveStatus?.duration_sec ?? 0) * 1000;
  const running = status === 'running';
  const elapsed = useElapsed(startedAt, running ? undefined : endAt, running);
  return <span className="font-mono text-[0.75rem] tabular-nums text-text-faint">{elapsed}</span>;
}

function Field({
  label,
  value,
  tone = 'muted',
}: {
  readonly label: string;
  readonly value: string;
  readonly tone?: 'muted' | 'danger';
}) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-[0.75rem] font-medium uppercase text-text-faint">{label}</dt>
      <dd className={tone === 'danger' ? 'text-danger' : 'text-text-muted'}>{value}</dd>
    </div>
  );
}
