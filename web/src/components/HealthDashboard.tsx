import { useCallback } from 'react';
import { Code2 } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';

const POLL_MS = 5000;

export function HealthDashboard() {
  const { t, formatRelative, formatNumber } = useLocale();
  const fetcher = useCallback(() => api.health(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher, POLL_MS);

  if (loading && !data) return <DashboardSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('health.errorTitle')} onRetry={refetch} />;
  if (!data) return null;

  const relTime = (iso: string) => relativeTime(iso, t, formatRelative);

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('health.title')}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">{t('health.subtitle')}</p>
        </div>
        {stale && <StalePill />}
      </header>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card title={t('health.wiki')} subtitle={relTime(data.wiki.last_update)}>
          <div className="text-3xl font-bold tabular-nums">{formatNumber(data.wiki.pages)}</div>
          <div className="text-xs text-muted-foreground">{t('health.pages')}</div>
        </Card>

        <Card title={t('health.sources')} subtitle={t('health.total', { n: total(data.sources.by_status) })}>
          <StatusBar
            buckets={data.sources.by_status}
            order={['stored', 'ocr_complete', 'ingested', 'failed']}
          />
        </Card>

        <Card
          title={t('health.scheduler')}
          subtitle={
            data.scheduler.next_run
              ? `next: ${relTime(data.scheduler.next_run)}`
              : t('health.noActiveTasks')
          }
        >
          <div className="text-3xl font-bold tabular-nums">
            {data.tasks.by_status['active'] ?? 0}
          </div>
          <div className="text-xs text-muted-foreground">{t('health.activeTasks')}</div>
        </Card>

        <SandboxCard sandbox={data.sandbox} />
        <EmbedCacheCard cache={data.embed_cache} />
        <TokenJuiceCard tj={data.tokenjuice} />
      </div>

      <ProcessFooter process={data.process} />
    </div>
  );
}

function SandboxCard({ sandbox }: { sandbox?: { enabled: boolean; available: boolean; runtime?: string; detail?: string } }) {
  const { t } = useLocale();
  const enabled = sandbox?.enabled ?? false;
  const available = sandbox?.available ?? false;
  const subtitle = !enabled
    ? t('health.sandboxDisabled')
    : available
      ? t('health.sandboxReady')
      : t('health.sandboxUnavailable');
  return (
    <Card title={t('health.sandbox')} subtitle={subtitle}>
      <div className="flex items-center gap-2">
        <Code2 size={20} className={available ? 'text-primary/70 shrink-0' : 'text-amber-500 shrink-0'} />
        <div className="text-3xl font-bold tabular-nums">
          {available ? t('health.on') : t('health.off')}
        </div>
      </div>
      <div className="mt-1 truncate text-xs text-muted-foreground" title={sandbox?.detail || sandbox?.runtime || ''}>
        {available ? (sandbox?.runtime || t('health.bundledRuntime')) : (sandbox?.detail || t('health.bundledRuntime'))}
      </div>
    </Card>
  );
}

function EmbedCacheCard({ cache }: { cache: { hits: number; misses: number } }) {
  const { t, formatNumber } = useLocale();
  const total = cache.hits + cache.misses;
  const hitRate = total === 0 ? null : Math.round((cache.hits / total) * 100);
  const subtitle = total === 0
    ? t('health.noEmbedsYet')
    : t('health.hitRate', { pct: String(hitRate) });
  return (
    <Card title={t('health.embedCache')} subtitle={subtitle}>
      <div className="text-3xl font-bold tabular-nums">{formatNumber(cache.hits)}</div>
      <div className="text-xs text-muted-foreground">
        {t('health.hits')} <span className="opacity-50">/</span> {formatNumber(cache.misses)} {t('health.misses')}
      </div>
    </Card>
  );
}

function TokenJuiceCard({ tj }: { tj?: { enabled: boolean; total_calls: number; total_bytes_saved: number; avg_ratio: number; top_rules_by_savings: Array<{ rule_id: string; bytes_saved: number }> } }) {
  const { t, formatNumber } = useLocale();
  const enabled = tj?.enabled ?? false;
  const calls = tj?.total_calls ?? 0;
  const bytesSaved = tj?.total_bytes_saved ?? 0;
  const subtitle = !enabled
    ? t('health.tokenJuiceDisabled')
    : calls === 0
      ? t('health.tokenJuiceNoActivity')
      : t('health.tokenJuiceEnabled');
  return (
    <Card title={t('health.tokenJuice')} subtitle={subtitle}>
      <div className="text-3xl font-bold tabular-nums">{formatNumber(calls)}</div>
      <div className="text-xs text-muted-foreground">
        {t('health.compactions')} <span className="opacity-50">/</span>{' '}
        {formatNumber(bytesSaved)} {t('health.bytesSaved')}
      </div>
    </Card>
  );
}

function ProcessFooter({ process: p }: { process: { version: string; git_revision?: string; started_at: string; uptime_seconds: number } }) {
  const { t, formatDate } = useLocale();
  if (!p?.version && !p?.git_revision) return null;
  return (
    <footer className="pt-4 mt-2 border-t flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
      <span>v{p.version || '?'}</span>
      {p.git_revision && <span className="font-mono">{p.git_revision}</span>}
      <span>{t('health.uptime')} {formatUptime(p.uptime_seconds)}</span>
      {p.started_at && !p.started_at.startsWith('0001') && (
        <span>{t('health.started')} {formatDate(p.started_at)}</span>
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
  const { t } = useLocale();
  const sum = total(buckets);
  if (sum === 0) {
    return <div className="text-sm text-muted-foreground">{t('health.noSourcesYet')}</div>;
  }
  const colors: Record<string, string> = {
    stored: 'aura-status-bar__segment--stored',
    ocr_complete: 'aura-status-bar__segment--ocr-complete',
    ingested: 'aura-status-bar__segment--ingested',
    failed: 'aura-status-bar__segment--failed',
  };
  const { segments } = order.reduce<{
    cursor: number;
    segments: Array<{ key: string; value: number; x: number; width: number }>;
  }>((acc, k) => {
    const v = buckets[k] ?? 0;
    if (v === 0) return acc;
    const width = (v / sum) * 100;
    return {
      cursor: acc.cursor + width,
      segments: [...acc.segments, { key: k, value: v, x: acc.cursor, width }],
    };
  }, { cursor: 0, segments: [] });
  return (
    <div className="space-y-2">
      <div
        className="aura-status-bar"
        aria-hidden="true"
      >
        <svg className="aura-status-bar__svg" viewBox="0 0 100 12" preserveAspectRatio="none" focusable="false">
          {segments.map(({ key, value, x, width }) => (
            <rect
              key={key}
              className={`aura-status-bar__segment ${colors[key] ?? 'aura-status-bar__segment--default'}`}
              x={x}
              y="0"
              width={width}
              height="12"
            >
              <title>{`${key}: ${value}`}</title>
            </rect>
          ))}
        </svg>
      </div>
      <span className="sr-only">
        {order.map((k) => `${k.replace('_', ' ')}: ${buckets[k] ?? 0}`).join(', ')}
      </span>
      <ul className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
        {order.map((k) => (
          <li key={k} className="flex items-center gap-2">
            <span aria-hidden="true" className={`aura-status-legend-swatch ${colors[k] ?? 'aura-status-bar__segment--default'}`} />
            <span className="text-foreground">{k.replace('_', ' ')}</span>
            <span className="ml-auto tabular-nums">{buckets[k] ?? 0}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function StalePill() {
  const { t } = useLocale();
  return (
    <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
      {t('common.stale')}
    </span>
  );
}

function total(b: Record<string, number>): number {
  return Object.values(b).reduce((a, c) => a + c, 0);
}

function relativeTime(
  iso: string,
  t: (key: string, options?: Record<string, unknown>) => string,
  formatRelative: (value: number, unit: Intl.RelativeTimeFormatUnit) => string,
): string {
  if (!iso || iso.startsWith('0001')) return t('health.never');
  const timestamp = new Date(iso).getTime();
  const diff = (Date.now() - timestamp) / 1000;
  if (diff < 0) {
    const inSec = -diff;
    if (inSec < 60) return formatRelative(Math.round(inSec), 'second');
    if (inSec < 3600) return formatRelative(Math.round(inSec / 60), 'minute');
    if (inSec < 86400) return formatRelative(Math.round(inSec / 3600), 'hour');
    return formatRelative(Math.round(inSec / 86400), 'day');
  }
  if (diff < 60) return t('health.justNow');
  if (diff < 3600) return formatRelative(-Math.round(diff / 60), 'minute');
  if (diff < 86400) return formatRelative(-Math.round(diff / 3600), 'hour');
  return formatRelative(-Math.round(diff / 86400), 'day');
}
