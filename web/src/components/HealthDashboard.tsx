import { useCallback } from 'react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';

const POLL_MS = 5000;

export function HealthDashboard() {
  const fetcher = useCallback(() => api.health(), []);
  const { data, error, loading, stale } = useApi(fetcher, POLL_MS);

  if (loading && !data) return <Loading />;
  if (error && !data) return <ErrorCard error={error} />;
  if (!data) return null;

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Dashboard</h1>
        {stale && <StalePill />}
      </header>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card title="Wiki" subtitle={relativeTime(data.wiki.last_update)}>
          <div className="text-3xl font-bold tabular-nums">{data.wiki.pages}</div>
          <div className="text-xs text-muted-foreground">pages</div>
        </Card>

        <Card title="Sources" subtitle={`${total(data.sources.by_status)} total`}>
          <StatusBar
            buckets={data.sources.by_status}
            order={['stored', 'ocr_complete', 'ingested', 'failed']}
          />
        </Card>

        <Card
          title="Scheduler"
          subtitle={
            data.scheduler.next_run
              ? `next: ${relativeTime(data.scheduler.next_run)}`
              : 'no active tasks'
          }
        >
          <div className="text-3xl font-bold tabular-nums">
            {data.tasks.by_status['active'] ?? 0}
          </div>
          <div className="text-xs text-muted-foreground">active tasks</div>
        </Card>
      </div>
    </div>
  );
}

function Card({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="flex items-baseline justify-between">
        <h2 className="text-sm font-medium text-muted-foreground">{title}</h2>
        {subtitle && <span className="text-xs text-muted-foreground">{subtitle}</span>}
      </div>
      <div className="mt-3">{children}</div>
    </div>
  );
}

function StatusBar({ buckets, order }: { buckets: Record<string, number>; order: string[] }) {
  const sum = total(buckets);
  if (sum === 0) {
    return <div className="text-sm text-muted-foreground">No sources yet</div>;
  }
  const colors: Record<string, string> = {
    stored: 'bg-zinc-400',
    ocr_complete: 'bg-blue-500',
    ingested: 'bg-emerald-500',
    failed: 'bg-rose-500',
  };
  return (
    <div className="space-y-2">
      <div className="flex h-3 overflow-hidden rounded bg-muted">
        {order.map((k) => {
          const v = buckets[k] ?? 0;
          if (v === 0) return null;
          return (
            <div
              key={k}
              className={`${colors[k] ?? 'bg-muted-foreground'}`}
              style={{ width: `${(v / sum) * 100}%` }}
              title={`${k}: ${v}`}
            />
          );
        })}
      </div>
      <ul className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
        {order.map((k) => (
          <li key={k} className="flex items-center gap-2">
            <span className={`inline-block w-2 h-2 rounded-sm ${colors[k] ?? 'bg-muted-foreground'}`} />
            <span className="text-muted-foreground">{k.replace('_', ' ')}</span>
            <span className="ml-auto tabular-nums">{buckets[k] ?? 0}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function StalePill() {
  return (
    <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
      ⚠ stale
    </span>
  );
}

function Loading() {
  return <div className="p-6 text-sm text-muted-foreground">Loading…</div>;
}
function ErrorCard({ error }: { error: Error }) {
  return (
    <div className="p-6">
      <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4">
        <h2 className="text-base font-semibold">Failed to load dashboard</h2>
        <p className="mt-2 text-sm text-muted-foreground">{error.message}</p>
      </div>
    </div>
  );
}

function total(b: Record<string, number>): number {
  return Object.values(b).reduce((a, c) => a + c, 0);
}

function relativeTime(iso: string): string {
  if (!iso || iso.startsWith('0001')) return 'never';
  const t = new Date(iso).getTime();
  const diff = (Date.now() - t) / 1000;
  if (diff < 0) {
    const inSec = -diff;
    if (inSec < 60) return `in ${Math.round(inSec)}s`;
    if (inSec < 3600) return `in ${Math.round(inSec / 60)}m`;
    if (inSec < 86400) return `in ${Math.round(inSec / 3600)}h`;
    return `in ${Math.round(inSec / 86400)}d`;
  }
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.round(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.round(diff / 3600)}h ago`;
  return `${Math.round(diff / 86400)}d ago`;
}
