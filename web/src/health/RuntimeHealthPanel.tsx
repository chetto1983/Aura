import { useRuntimeHealth, type HealthzBody, type ReadyzBody } from './useRuntimeHealth';

type Tone = 'success' | 'warning' | 'danger';

interface RowState {
  label: string;
  status: string;
  tone: Tone;
  mono?: boolean;
}

const TONE_CLASS: Record<Tone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

// StatusDot is NEVER rendered alone — its colour is decorative (aria-hidden) and
// the sibling text label in StatusRow carries the meaning (WCAG 1.4.1).
function StatusDot({ tone }: { tone: Tone }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-block h-2 w-2 shrink-0 rounded-sm ${TONE_CLASS[tone]}`}
    />
  );
}

function StatusRow({ label, status, tone, mono }: RowState) {
  return (
    <div className="flex min-h-[var(--row-h)] items-center justify-between gap-4 py-1">
      <span className="text-sm text-text-muted">{label}</span>
      <span className="flex items-center gap-2">
        <StatusDot tone={tone} />
        <span className={`text-sm text-text ${mono ? 'font-mono' : ''}`}>{status}</span>
      </span>
    </div>
  );
}

function liveness(
  healthz: { status: number; body: HealthzBody } | undefined,
  errored: boolean,
): RowState {
  if (errored || !healthz) {
    return { label: 'Liveness', status: 'Unavailable', tone: 'danger' };
  }
  if (healthz.status === 200 && healthz.body.ok) {
    return { label: 'Liveness', status: 'Live', tone: 'success' };
  }
  return { label: 'Liveness', status: 'Unavailable', tone: 'danger' };
}

function readiness(
  readyz: { status: number; body: ReadyzBody } | undefined,
  errored: boolean,
): RowState {
  if (errored || !readyz) {
    return { label: 'Readiness', status: 'Unavailable', tone: 'danger' };
  }
  if (readyz.status === 200 && readyz.body.ready) {
    return { label: 'Readiness', status: 'Ready', tone: 'success' };
  }
  // A 503 with a deps map means the probe ran but a dependency is not ready.
  return { label: 'Readiness', status: 'Degraded', tone: 'warning' };
}

function dependency(
  label: string,
  depKey: string,
  readyz: { status: number; body: ReadyzBody } | undefined,
  errored: boolean,
): RowState {
  if (errored || !readyz) {
    return { label, status: 'Unavailable', tone: 'danger' };
  }
  const value = readyz.body.deps[depKey];
  if (value === undefined) {
    return { label, status: 'Unknown', tone: 'warning' };
  }
  if (value === 'ok') {
    return { label, status: 'Ready', tone: 'success' };
  }
  return { label, status: 'Degraded', tone: 'warning' };
}

function formatLastChecked(epochMs: number): string {
  if (epochMs === 0) {
    return 'never';
  }
  const seconds = Math.max(0, Math.round((Date.now() - epochMs) / 1000));
  if (seconds < 5) {
    return 'just now';
  }
  if (seconds < 60) {
    return `${String(seconds)}s ago`;
  }
  const minutes = Math.round(seconds / 60);
  return `${String(minutes)}m ago`;
}

export function RuntimeHealthPanel() {
  const { healthz, readyz, healthzError, readyzError, isPending, lastChecked } = useRuntimeHealth();

  const allUnreachable =
    healthzError && readyzError && healthz === undefined && readyz === undefined;

  const bind = healthz?.body.bind_address;
  const build = healthz?.body.build_version;

  const rows: RowState[] = [
    liveness(healthz, healthzError),
    readiness(readyz, readyzError),
    dependency('Postgres', 'postgres', readyz, readyzError),
    dependency('Neo4j', 'neo4j', readyz, readyzError),
  ];

  return (
    <section aria-labelledby="runtime-health-title" className="flex flex-col gap-2 p-4">
      <h2 id="runtime-health-title" className="text-base font-medium text-text">
        Runtime
      </h2>

      {isPending ? (
        <p className="text-sm text-text-muted">Checking runtime…</p>
      ) : allUnreachable ? (
        <p role="alert" className="text-sm text-danger">
          Can&apos;t read runtime status. The health endpoints aren&apos;t responding — the server
          may be starting or down.
        </p>
      ) : (
        <div className="flex flex-col">
          {rows.map((row) => (
            <StatusRow key={row.label} {...row} />
          ))}
          <StatusRow label="Bind address" status={bind ?? '—'} tone="success" mono />
          <StatusRow label="Build" status={build ?? '—'} tone="success" mono />
        </div>
      )}

      <p className="text-[0.6875rem] text-text-faint">
        Last checked {formatLastChecked(lastChecked)}
      </p>
    </section>
  );
}
