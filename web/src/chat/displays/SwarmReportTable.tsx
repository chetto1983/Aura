import { useEffect, useId, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import {
  ChevronRight,
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

// Keep worker registration mounted even when the parent's tool call is complete:
// these durable children have a lifecycle independent of that call.

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
  const label = t('swarm.team');

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
    <DisplayCardShell label={label} meta={String(reports.length)}>
      <ul
        aria-label={t('swarm.picker.label')}
        className="grid min-w-0 gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(100%,20rem),1fr))]"
      >
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
      </ul>
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
  const goal = report.goal ?? t('swarm.agentNumber', { number: report.goal_index + 1 });
  const summary =
    (status === 'failed' || status === 'dead_letter') && hasField(report.error)
      ? report.error
      : (report.summary ?? t('swarm.noSummary'));

  return (
    <li
      data-worker-id={report.child_id}
      className="flex min-w-0 flex-col rounded-[var(--radius-md)] border border-border bg-surface p-3"
    >
      <div className="mb-2 flex items-center justify-between gap-2 text-xs">
        <span className="flex items-center gap-2 text-text-muted">
          <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${dotClass}`} />
          {t('swarm.agentNumber', { number: report.goal_index + 1 })}
        </span>
        <span className="flex items-center gap-2 text-text-muted">
          <StatusIcon status={status} />
          <span>{statusLabel}</span>
          <WorkerDuration status={status} liveStatus={liveStatus} />
        </span>
      </div>
      <Button
        type="button"
        variant="ghost"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-label={t('swarm.expand')}
        className="h-auto min-h-[44px] w-full flex-col items-start gap-2 whitespace-normal px-0 py-1 text-left hover:bg-surface-2"
      >
        <span
          className="line-clamp-3 break-words text-sm font-medium leading-relaxed text-text"
          title={goal}
        >
          {goal}
        </span>
        <span className="font-mono text-xs text-text-faint">{report.child_id}</span>
        <span
          className="line-clamp-2 break-words text-xs font-normal leading-relaxed text-text-muted"
          title={summary}
        >
          {cap(summary, 300)}
        </span>
      </Button>
      <div className="mt-auto flex flex-wrap items-center justify-between gap-1 pt-2">
        <Button
          type="button"
          variant="ghost"
          onClick={onWatch}
          data-required-touch-target
          className="min-h-[44px] min-w-[44px] rounded-none px-3 text-accent-text focus-visible:ring-2 focus-visible:ring-accent"
        >
          {t('swarm.watch')}
          <ChevronRight className="size-4" aria-hidden="true" />
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
      {expanded ? (
        <div className="mt-2 break-words border-t border-border py-3">
          <dl className="flex flex-col gap-2 text-sm">
            <Field label={t('swarm.columns.goal')} value={goal} />
            <Field label={t('swarm.summaryLabel')} value={report.summary ?? t('swarm.noSummary')} />
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
        </div>
      ) : null}
    </li>
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
