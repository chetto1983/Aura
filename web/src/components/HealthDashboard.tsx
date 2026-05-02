import { useCallback } from 'react';
import { TrendingUp } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import type { CompoundingRate } from '@/types/api';

const POLL_MS = 5000;

export function HealthDashboard() {
  const fetcher = useCallback(() => api.health(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher, POLL_MS);

  if (loading && !data) return <DashboardSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Failed to load dashboard" onRetry={refetch} />;
  if (!data) return null;

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
          <p className="text-xs text-muted-foreground mt-0.5">Live health rollup · refreshes every 5s</p>
        </div>
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

        <EmbedCacheCard cache={data.embed_cache} />
        {data.compounding_rate && (
          <CompoundingRateCard rate={data.compounding_rate} />
        )}
      </div>

      <ProcessFooter process={data.process} />
    </div>
  );
}

function EmbedCacheCard({ cache }: { cache: { hits: number; misses: number } }) {
  const total = cache.hits + cache.misses;
  const hitRate = total === 0 ? null : Math.round((cache.hits / total) * 100);
  const subtitle = total === 0
    ? 'no embeds yet'
    : `${hitRate}% hit rate`;
  return (
    <Card title="Embed cache" subtitle={subtitle}>
      <div className="text-3xl font-bold tabular-nums">{cache.hits.toLocaleString()}</div>
      <div className="text-xs text-muted-foreground">
        hits <span className="opacity-50">/</span> {cache.misses.toLocaleString()} miss{cache.misses === 1 ? '' : 'es'}
      </div>
    </Card>
  );
}

function CompoundingRateCard({ rate }: { rate: CompoundingRate }) {
  const pct = rate.rate_pct;
  const formatted = pct < 1 && pct > 0
    ? `${pct.toFixed(1)}%`
    : `${Math.round(pct)}%`;
  const subtitle = `${rate.auto_added_7d} pages auto-added this week / ${rate.total_pages} total`;
  return (
    <Card title="Compounding rate" subtitle={subtitle}>
      <div
        className="flex items-center gap-2"
        title="Pages added by Aura's auto-summarizer in the last 7 days"
      >
        <TrendingUp size={20} className="text-primary/70 shrink-0" />
        <div className="text-3xl font-bold tabular-nums">{formatted}</div>
      </div>
      <div className="text-xs text-muted-foreground">auto-summarizer growth</div>
    </Card>
  );
}

function ProcessFooter({ process: p }: { process: { version: string; git_revision?: string; started_at: string; uptime_seconds: number } }) {
  if (!p?.version && !p?.git_revision) return null;
  return (
    <footer className="pt-4 mt-2 border-t flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
      <span>v{p.version || '?'}</span>
      {p.git_revision && <span className="font-mono">{p.git_revision}</span>}
      <span>uptime {formatUptime(p.uptime_seconds)}</span>
      {p.started_at && !p.started_at.startsWith('0001') && (
        <span>started {new Date(p.started_at).toLocaleString()}</span>
      )}
    </footer>
  );
}

function DashboardSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-40" />
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {[0, 1, 2].map((i) => (
          <div key={i} className="rounded-lg border bg-card p-4 space-y-3">
            <div className="flex items-baseline justify-between">
              <Skeleton className="h-4 w-16" />
              <Skeleton className="h-3 w-12" />
            </div>
            <Skeleton className="h-9 w-20" />
            <Skeleton className="h-3 w-24" />
          </div>
        ))}
      </div>
    </div>
  );
}

function formatUptime(seconds: number): string {
  if (!seconds || seconds < 1) return '—';
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${seconds}s`;
}

function Card({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <div className="group relative rounded-xl border bg-card p-5 transition-colors hover:border-primary/30">
      {/* Subtle top-left accent stripe — picks up the brand color on hover */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-5 top-0 h-px bg-gradient-to-r from-primary/40 via-primary/10 to-transparent opacity-0 transition-opacity group-hover:opacity-100"
      />
      <div className="flex items-baseline justify-between">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{title}</h2>
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
    stored: 'bg-slate-400/80',
    ocr_complete: 'bg-sky-400',
    ingested: 'bg-primary',
    failed: 'bg-rose-500',
  };
  return (
    <div className="space-y-2">
      <div
        className="flex h-3 overflow-hidden rounded bg-muted"
        role="img"
        aria-label={order.map((k) => `${k.replace('_', ' ')}: ${buckets[k] ?? 0}`).join(', ')}
      >
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
