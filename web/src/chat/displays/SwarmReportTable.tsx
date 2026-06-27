import { useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import type { DisplayChildReport } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import { hasField, hasOptions, statusDotClass, statusLabelKey } from './swarmRow';
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
  const dotClass = statusDotClass(report.status);
  const statusLabel = t(statusLabelKey(report.status));

  return (
    <>
      <tr>
        <td colSpan={4} className="border-b border-border p-0">
          <Button
            type="button"
            variant="ghost"
            onClick={onToggle}
            aria-expanded={expanded}
            aria-label={t('swarm.expand')}
            className="grid h-auto min-h-11 w-full grid-cols-[3rem_1fr_8rem_2fr] justify-normal gap-2 rounded-none px-3 py-2 text-left hover:bg-surface"
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
              <span className="text-[0.75rem] font-medium text-text">{statusLabel}</span>
            </span>
            <span className="truncate text-sm text-text-muted">
              {report.summary ?? t('swarm.noSummary')}
            </span>
          </Button>
        </td>
      </tr>
      {expanded ? (
        <tr>
          <td colSpan={4} className="border-b border-border bg-surface px-3 py-2">
            <dl className="flex flex-col gap-2 text-sm">
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
