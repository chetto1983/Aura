import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { RuntimeHealthPanelSkeleton } from '../components/skeleton';
import { useRuntimeHealth, type HealthzBody, type ReadyzBody } from './useRuntimeHealth';

type Tone = 'success' | 'warning' | 'danger';

interface RowState {
  label: string;
  status: string;
  tone: Tone;
  mono?: boolean;
  title?: string;
}

const TONE_CLASS: Record<Tone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

// StatusDot is NEVER rendered alone - its colour is decorative (aria-hidden) and
// the sibling text label in StatusRow carries the meaning (WCAG 1.4.1).
function StatusDot({ tone }: { tone: Tone }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-block h-2 w-2 shrink-0 rounded-sm ${TONE_CLASS[tone]}`}
    />
  );
}

function StatusRow({ label, status, tone, mono, title }: RowState) {
  return (
    <div className="flex min-h-[var(--row-h)] items-center justify-between gap-4 py-1">
      <span className="text-sm text-text-muted">{label}</span>
      <span className="flex items-center gap-2">
        <StatusDot tone={tone} />
        <span title={title} className={`text-sm text-text ${mono ? 'font-mono' : ''}`}>
          {status}
        </span>
      </span>
    </div>
  );
}

function shortCommit(commit: string): string {
  return commit.slice(0, 12);
}

function liveness(
  healthz: { status: number; body: HealthzBody } | undefined,
  errored: boolean,
  t: TFunction,
): RowState {
  if (errored || !healthz) {
    return {
      label: t('health.labels.liveness'),
      status: t('health.status.unavailable'),
      tone: 'danger',
    };
  }
  if (healthz.status === 200 && healthz.body.ok) {
    return { label: t('health.labels.liveness'), status: t('health.status.live'), tone: 'success' };
  }
  return {
    label: t('health.labels.liveness'),
    status: t('health.status.unavailable'),
    tone: 'danger',
  };
}

function readiness(
  readyz: { status: number; body: ReadyzBody } | undefined,
  errored: boolean,
  t: TFunction,
): RowState {
  if (errored || !readyz) {
    return {
      label: t('health.labels.readiness'),
      status: t('health.status.unavailable'),
      tone: 'danger',
    };
  }
  if (readyz.status === 200 && readyz.body.ready) {
    return {
      label: t('health.labels.readiness'),
      status: t('health.status.ready'),
      tone: 'success',
    };
  }
  // A 503 means the probes ran and at least one reason holds; ReasonList names them.
  return {
    label: t('health.labels.readiness'),
    status: t('health.status.degraded'),
    tone: 'warning',
  };
}

// A daemon newer than this bundle can report a code it has no copy for, so the code
// itself is the fallback title: the operator can still search the logs for it.
function ReasonList({ reasons }: { reasons: string[] }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 border-t border-border py-2">
      <h3 id="runtime-health-why" className="text-xs font-medium text-text-muted">
        {t('health.why')}
      </h3>
      <ul aria-labelledby="runtime-health-why" className="flex flex-col gap-2">
        {reasons.map((code) => (
          <li key={code} className="flex items-baseline gap-2">
            <StatusDot tone="warning" />
            <span className="min-w-0">
              <span className="block text-sm text-text">
                {t(`health.reasons.${code}.title`, { defaultValue: code })}
              </span>
              <span className="block text-xs text-text-muted">
                {t(`health.reasons.${code}.detail`, {
                  defaultValue: t('health.reasons.unknown'),
                })}
              </span>
            </span>
          </li>
        ))}
      </ul>
      <p className="text-[0.75rem] text-text-faint">{t('health.reasonsHint')}</p>
    </div>
  );
}

function formatLastChecked(epochMs: number, t: TFunction): string {
  if (epochMs === 0) {
    return t('health.relative.never');
  }
  const seconds = Math.max(0, Math.round((Date.now() - epochMs) / 1000));
  if (seconds < 5) {
    return t('health.relative.justNow');
  }
  if (seconds < 60) {
    return t('health.relative.secondsAgo', { count: seconds });
  }
  const minutes = Math.round(seconds / 60);
  return t('health.relative.minutesAgo', { count: minutes });
}

export function RuntimeHealthPanel() {
  const { t } = useTranslation();
  const { healthz, readyz, healthzError, readyzError, isPending, isRefetching, lastChecked } =
    useRuntimeHealth();

  const allUnreachable =
    healthzError && readyzError && healthz === undefined && readyz === undefined;

  const bind = healthz?.body.bind_address;
  const build = healthz?.body.build_version;
  const buildCommit = healthz?.body.build_commit;
  const buildDate = healthz?.body.build_date;

  const rows: RowState[] = [liveness(healthz, healthzError, t), readiness(readyz, readyzError, t)];
  const reasons = readyzError ? [] : (readyz?.body.reasons ?? []);

  return (
    <section
      aria-busy={isPending || isRefetching}
      aria-labelledby="runtime-health-title"
      className="flex flex-col gap-2 p-4"
    >
      {isRefetching ? <div aria-hidden="true" className="skeleton-refetch-bar" /> : null}
      <h2 id="runtime-health-title" className="text-base font-medium text-text">
        {t('health.title')}
      </h2>

      {isPending ? (
        <RuntimeHealthPanelSkeleton label={t('health.loading')} showTitle={false} />
      ) : allUnreachable ? (
        <p role="alert" className="text-sm text-danger">
          {t('health.unreachable')}
        </p>
      ) : (
        <>
          <div className="flex flex-col">
            {rows.map((row) => (
              <StatusRow key={row.label} {...row} />
            ))}
            {reasons.length > 0 ? <ReasonList reasons={reasons} /> : null}
            <StatusRow
              label={t('health.labels.bindAddress')}
              status={bind ?? '-'}
              tone="success"
              mono
            />
            <StatusRow label={t('health.labels.build')} status={build ?? '-'} tone="success" mono />
            {buildCommit ? (
              <StatusRow
                label={t('health.labels.commit')}
                status={shortCommit(buildCommit)}
                title={buildCommit}
                tone="success"
                mono
              />
            ) : null}
            {buildDate ? (
              <StatusRow
                label={t('health.labels.buildDate')}
                status={buildDate}
                tone="success"
                mono
              />
            ) : null}
          </div>
        </>
      )}

      <p className="text-[0.75rem] text-text-faint">
        {t('health.lastChecked', { time: formatLastChecked(lastChecked, t) })}
      </p>
    </section>
  );
}
