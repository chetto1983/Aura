import { useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import type { DisplayChildReport } from './types';
import { DisplayCardShell } from './DisplayCardShell';

// SwarmReportTable (SWARM-01 / D-08): a summary table over the swarm []ChildReport
// payload — one row per worker (# goal-index / Worker child-id / Status dot+label /
// Summary). Clicking a row expands its summary + error (+ question/options for a
// needs_user_input child) IN PLACE. Status comes from the Status enum ONLY (ok /
// failed / needs_user_input), never the free-form Error text, and is conveyed by
// dot + icon + text (color is never the only signal). There is deliberately NO
// inter-agent chat / mailbox affordance (D-08); the full per-child .jsonl transcript
// drill-down is a deferred follow-up.

type SwarmStatus = 'ok' | 'failed' | 'needs_user_input';

const DOT_CLASS: Record<SwarmStatus, string> = {
  ok: 'bg-success',
  failed: 'bg-danger',
  needs_user_input: 'bg-warning',
};

const STATUS_KEY: Record<SwarmStatus, string> = {
  ok: 'swarm.status.ok',
  failed: 'swarm.status.failed',
  needs_user_input: 'swarm.status.needs_user_input',
};

function isSwarmStatus(value: string): value is SwarmStatus {
  return value === 'ok' || value === 'failed' || value === 'needs_user_input';
}

export interface SwarmReportTableProps {
  readonly payload: { readonly swarm?: readonly DisplayChildReport[] };
}

export function SwarmReportTable({ payload }: SwarmReportTableProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<number | null>(null);
  const reports = payload.swarm ?? [];
  const label = t('display.type.swarm_report');

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
        <table className="min-w-full border-collapse text-left text-sm">
          <thead>
            <tr>
              {[
                t('swarm.columns.index'),
                t('swarm.columns.worker'),
                t('swarm.columns.status'),
                t('swarm.columns.summary'),
              ].map((col) => (
                <th
                  key={col}
                  className="border-b border-border bg-surface-2 px-3 py-2 text-[0.75rem] font-medium uppercase text-text-faint"
                >
                  {col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {reports.map((r, i) => {
              const expanded = open === i;
              return (
                <SwarmRow
                  key={`${String(r.goal_index)}-${r.child_id}`}
                  report={r}
                  expanded={expanded}
                  onToggle={() => {
                    setOpen(expanded ? null : i);
                  }}
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
  readonly expanded: boolean;
  readonly onToggle: () => void;
  readonly t: TFunction;
}

function SwarmRow({ report, expanded, onToggle, t }: SwarmRowProps) {
  const status: SwarmStatus = isSwarmStatus(report.status) ? report.status : 'failed';
  const statusLabel = isSwarmStatus(report.status)
    ? t(STATUS_KEY[status])
    : t('swarm.status.unknown');

  return (
    <>
      <tr>
        <td colSpan={4} className="border-b border-border p-0">
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={expanded}
            aria-label={t('swarm.expand')}
            className="grid w-full grid-cols-[3rem_1fr_8rem_2fr] items-center gap-2 px-3 py-2 text-left hover:bg-surface focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
          >
            <span className="font-mono text-[0.75rem] tabular-nums text-text-faint">
              {report.goal_index}
            </span>
            <span className="truncate font-mono text-xs text-text-muted">{report.child_id}</span>
            <span className="flex items-center gap-2">
              <span
                aria-hidden="true"
                className={`inline-block h-2 w-2 shrink-0 rounded-sm ${DOT_CLASS[status]}`}
              />
              <span className="text-[0.75rem] font-medium text-text">{statusLabel}</span>
            </span>
            <span className="truncate text-sm text-text-muted">
              {report.summary ?? t('swarm.noSummary')}
            </span>
          </button>
        </td>
      </tr>
      {expanded ? (
        <tr>
          <td colSpan={4} className="border-b border-border bg-surface px-3 py-2">
            <dl className="flex flex-col gap-2 text-sm">
              <Field label={t('swarm.summaryLabel')} value={report.summary ?? t('swarm.noSummary')} />
              {report.error !== undefined && report.error !== '' ? (
                <Field label={t('swarm.errorLabel')} value={report.error} tone="danger" />
              ) : null}
              {report.question !== undefined && report.question !== '' ? (
                <Field label={t('swarm.questionLabel')} value={report.question} />
              ) : null}
              {report.options !== undefined && report.options.length > 0 ? (
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
