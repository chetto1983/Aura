import { useCallback, useState } from 'react';
import { Wrench, ExternalLink } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
import { ErrorCard } from '@/components/common/ErrorCard';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { WikiIssue, AuthzResponse } from '@/types/api';

const SEVERITY_ORDER = ['high', 'medium', 'low'] as const;

const SEVERITY_COLOR: Record<string, string> = {
  high: 'bg-rose-500/15 text-rose-600 dark:text-rose-400 border-rose-500/20',
  medium: 'bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/20',
  low: 'bg-sky-500/15 text-sky-600 dark:text-sky-400 border-sky-500/20',
};

const SEVERITY_SECTION: Record<string, string> = {
  high: 'border-rose-500/20',
  medium: 'border-amber-500/20',
  low: 'border-sky-500/20',
};

type SeverityFilter = 'all' | 'high' | 'medium' | 'low';
type StatusFilter = 'open' | 'resolved' | 'all';

export function MaintenancePanel() {
  const { t } = useLocale();
  const [activeTab, setActiveTab] = useState<'issues' | 'authz'>('issues');

  return (
    <div className="p-6 space-y-4">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">{t('maintenance.title')}</h1>
        <p className="text-xs text-muted-foreground mt-0.5">{t('maintenance.subtitle')}</p>
      </header>

      <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as 'issues' | 'authz')}>
        <TabsList>
          <TabsTrigger value="issues">Issues</TabsTrigger>
          <TabsTrigger value="authz">Authz</TabsTrigger>
        </TabsList>
        <TabsContent value="issues">
          <IssuesTab t={t} />
        </TabsContent>
        <TabsContent value="authz">
          <AuthzTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// --- Issues tab (original content) ---

function IssuesTab({ t }: { t: ReturnType<typeof useLocale>['t'] }) {
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('all');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('open');
  const [dismissed, setDismissed] = useState<Set<number>>(new Set());

  const fetcher = useCallback(
    () =>
      api.maintenanceIssues(
        statusFilter !== 'all' ? statusFilter : undefined,
        severityFilter !== 'all' ? severityFilter : undefined,
      ),
    [severityFilter, statusFilter],
  );

  const { data, error, loading, refetch } = useApi<WikiIssue[]>(fetcher);

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('maintenance.errorTitle')} onRetry={refetch} />;

  const issues = (data ?? []).filter((i) => !dismissed.has(i.id));

  const grouped = SEVERITY_ORDER.reduce<Record<string, WikiIssue[]>>(
    (acc, sev) => {
      acc[sev] = issues.filter((i) => i.severity === sev);
      return acc;
    },
    { high: [], medium: [], low: [] },
  );

  const total = issues.length;

  return (
    <div className="mt-4 space-y-4">
      <div className="flex gap-2 flex-wrap">
        <select
          aria-label={t('maintenance.statusFilterLabel')}
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
          className="min-h-11 rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
        >
          <option value="open">{t('maintenance.filterOpen')}</option>
          <option value="resolved">{t('maintenance.filterResolved')}</option>
          <option value="all">{t('maintenance.filterAllStatuses')}</option>
        </select>
        <select
          aria-label={t('maintenance.severityFilterLabel')}
          value={severityFilter}
          onChange={(e) => setSeverityFilter(e.target.value as SeverityFilter)}
          className="min-h-11 rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
        >
          <option value="all">{t('maintenance.filterAllSeverities')}</option>
          <option value="high">{t('maintenance.filterHigh')}</option>
          <option value="medium">{t('maintenance.filterMedium')}</option>
          <option value="low">{t('maintenance.filterLow')}</option>
        </select>
      </div>

      {total === 0 ? (
        <EmptyState />
      ) : (
        <div className="space-y-6">
          {SEVERITY_ORDER.map((sev) => {
            const group = grouped[sev];
            if (group.length === 0) return null;
            return (
              <section key={sev}>
                <h2 className={`text-xs font-semibold uppercase tracking-wider mb-2 ${SEVERITY_COLOR[sev].split(' ')[1]}`}>
                  {severityLabel(sev, t)} ({group.length})
                </h2>
                <div className="space-y-3">
                  {group.map((issue) => (
                    <IssueCard
                      key={issue.id}
                      issue={issue}
                      onDismiss={() => setDismissed((prev) => new Set(prev).add(issue.id))}
                    />
                  ))}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}

// --- Authz tab ---

function AuthzTab() {
  const { data, error, loading, refetch } = useApi<AuthzResponse>(() => api.maintenanceAuthz());

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Authz error" onRetry={refetch} />;

  const stats = data ?? { denial_rate_24h: 0, top_denied_capabilities: [], recent_denials: [] };
  const ratePercent = (stats.denial_rate_24h * 100).toFixed(1);

  return (
    <div className="mt-4 space-y-6">
      {/* Section 1: Denial rate */}
      <section className="rounded-xl border bg-card p-5 space-y-1">
        <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Denial Rate (24h)</h2>
        <p className="text-3xl font-bold tabular-nums">
          {ratePercent}%
        </p>
        <p className="text-xs text-muted-foreground">
          Proportion of authz decisions that were denied in the last 24 hours.
        </p>
      </section>

      {/* Section 2: Top denied capabilities */}
      <section className="rounded-xl border bg-card p-5 space-y-3">
        <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
          Top Denied Capabilities (24h)
        </h2>
        {stats.top_denied_capabilities.length === 0 ? (
          <p className="text-sm text-muted-foreground">No denials in the last 24 hours.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted-foreground border-b">
                <th className="pb-2 font-medium">Capability</th>
                <th className="pb-2 font-medium text-right">Count</th>
              </tr>
            </thead>
            <tbody>
              {stats.top_denied_capabilities.map((c) => (
                <tr key={c.capability} className="border-b last:border-0">
                  <td className="py-2 font-mono text-xs">{c.capability}</td>
                  <td className="py-2 text-right tabular-nums font-semibold">{c.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {/* Section 3: Recent denials */}
      <section className="rounded-xl border bg-card p-5 space-y-3">
        <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
          Recent Denials (last 50)
        </h2>
        {stats.recent_denials.length === 0 ? (
          <p className="text-sm text-muted-foreground">No denied decisions on record.</p>
        ) : (
          <div className="space-y-2 max-h-96 overflow-y-auto">
            {stats.recent_denials.map((d, i) => (
              <div key={i} className="rounded-lg border bg-muted/30 p-3 space-y-1">
                <div className="flex items-center justify-between gap-2 flex-wrap">
                  <span className="font-mono text-xs text-muted-foreground">{d.actor_id}</span>
                  <span className="text-xs text-muted-foreground tabular-nums">{d.created_at}</span>
                </div>
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="inline-flex rounded-full bg-rose-500/15 text-rose-600 dark:text-rose-400 border border-rose-500/20 px-2.5 py-0.5 text-xs font-medium font-mono">
                    {d.capability}
                  </span>
                  {d.reason && (
                    <span className="text-xs text-muted-foreground">{d.reason}</span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function IssueCard({ issue, onDismiss }: { issue: WikiIssue; onDismiss: () => void }) {
  const { t, formatDate } = useLocale();
  const [resolving, setResolving] = useState(false);
  const borderClass = SEVERITY_SECTION[issue.severity] ?? '';

  const handleResolve = async () => {
    if (resolving) return;
    setResolving(true);
    const tid = toast.loading(t('maintenance.toast.resolving'));
    try {
      await api.resolveIssue(issue.id);
      toast.success(t('maintenance.toast.resolved'), { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('maintenance.toast.resolveFailed'), { id: tid });
      setResolving(false);
    }
  };

  return (
    <div className={`rounded-xl border bg-card p-5 space-y-3 ${borderClass}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span className={`inline-flex rounded-full px-2.5 py-0.5 text-xs font-medium border ${SEVERITY_COLOR[issue.severity] ?? 'bg-muted text-muted-foreground'}`}>
              {severityLabel(issue.severity, t)}
            </span>
            <span className="text-xs text-muted-foreground font-mono">{issue.kind.replace(/_/g, ' ')}</span>
          </div>
          {issue.message && (
            <p className="text-sm text-muted-foreground leading-relaxed">{issue.message}</p>
          )}
          {issue.broken_link && (
            <p className="text-sm">
              {t('maintenance.brokenLink')}{' '}
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{issue.broken_link}</code>
            </p>
          )}
        </div>
        <span className={`shrink-0 text-xs rounded-full px-2 py-0.5 ${
          issue.status === 'resolved'
            ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
            : 'bg-muted text-muted-foreground'
        }`}>
          {statusLabel(issue.status, t)}
        </span>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        {issue.status !== 'resolved' && (
          <button
            type="button"
            disabled={resolving}
            onClick={() => void handleResolve()}
            className="min-h-11 rounded-md bg-primary/10 hover:bg-primary/20 text-primary px-3 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-wait"
          >
            {resolving ? t('maintenance.resolving') : t('maintenance.markResolved')}
          </button>
        )}
        {issue.slug && (
          <a
            href={`/wiki/${encodeURIComponent(issue.slug)}`}
            className="inline-flex min-h-11 items-center gap-1 rounded-md bg-muted hover:bg-muted/80 text-muted-foreground px-3 py-2 text-sm font-medium transition-colors"
          >
            {t('maintenance.openPage')}
            <ExternalLink size={10} />
          </a>
        )}
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">
          {formatDate(issue.created_at)}
        </span>
      </div>
    </div>
  );
}

function severityLabel(severity: string, t: ReturnType<typeof useLocale>['t']): string {
  switch (severity) {
    case 'high': return t('maintenance.severity.high');
    case 'medium': return t('maintenance.severity.medium');
    case 'low': return t('maintenance.severity.low');
    default: return severity;
  }
}

function statusLabel(status: string, t: ReturnType<typeof useLocale>['t']): string {
  switch (status) {
    case 'open': return t('maintenance.status.open');
    case 'resolved': return t('maintenance.status.resolved');
    default: return status;
  }
}

function EmptyState() {
  const { t } = useLocale();
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
      <Wrench size={40} className="text-muted-foreground/40" />
      <p className="text-sm font-medium text-muted-foreground">{t('maintenance.emptyTitle')}</p>
      <p className="text-sm text-muted-foreground">{t('maintenance.emptyHint')}</p>
    </div>
  );
}

function PanelSkeleton() {
  return (
    <div className="mt-4 space-y-4">
      <Skeleton className="h-8 w-48" />
      {[0, 1, 2].map((i) => (
        <div key={i} className="rounded-xl border p-5 space-y-3">
          <div className="flex gap-2">
            <Skeleton className="h-5 w-14" />
            <Skeleton className="h-5 w-20" />
          </div>
          <Skeleton className="h-4 w-full" />
          <div className="flex gap-2">
            <Skeleton className="h-7 w-28" />
            <Skeleton className="h-7 w-20" />
          </div>
        </div>
      ))}
    </div>
  );
}
