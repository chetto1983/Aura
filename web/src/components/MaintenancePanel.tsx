import { useCallback, useState } from 'react';
import { Wrench, ExternalLink } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { WikiIssue } from '@/types/api';

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
  if (error && !data) return <ErrorCard error={error} title="Failed to load maintenance issues" onRetry={refetch} />;

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
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Maintenance</h1>
          <p className="text-xs text-muted-foreground mt-0.5">Wiki issue queue</p>
        </div>
        <div className="flex gap-2 flex-wrap">
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
            className="rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
          >
            <option value="open">Open</option>
            <option value="resolved">Resolved</option>
            <option value="all">All statuses</option>
          </select>
          <select
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value as SeverityFilter)}
            className="rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
          >
            <option value="all">All severities</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
        </div>
      </header>

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
                  {sev} ({group.length})
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

function IssueCard({ issue, onDismiss }: { issue: WikiIssue; onDismiss: () => void }) {
  const [resolving, setResolving] = useState(false);
  const borderClass = SEVERITY_SECTION[issue.severity] ?? '';

  const handleResolve = async () => {
    if (resolving) return;
    setResolving(true);
    const tid = toast.loading('Resolving…');
    try {
      await api.resolveIssue(issue.id);
      toast.success('Issue marked resolved.', { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to resolve', { id: tid });
      setResolving(false);
    }
  };

  return (
    <div className={`rounded-xl border bg-card p-5 space-y-3 ${borderClass}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span className={`inline-flex rounded-full px-2.5 py-0.5 text-xs font-medium border ${SEVERITY_COLOR[issue.severity] ?? 'bg-muted text-muted-foreground'}`}>
              {issue.severity}
            </span>
            <span className="text-xs text-muted-foreground font-mono">{issue.kind.replace(/_/g, ' ')}</span>
          </div>
          {issue.message && (
            <p className="text-sm text-muted-foreground leading-relaxed">{issue.message}</p>
          )}
          {issue.broken_link && (
            <p className="text-sm">
              Broken link:{' '}
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{issue.broken_link}</code>
            </p>
          )}
        </div>
        <span className={`shrink-0 text-xs rounded-full px-2 py-0.5 ${
          issue.status === 'resolved'
            ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
            : 'bg-muted text-muted-foreground'
        }`}>
          {issue.status}
        </span>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        {issue.status !== 'resolved' && (
          <button
            type="button"
            disabled={resolving}
            onClick={() => void handleResolve()}
            className="rounded-md bg-primary/10 hover:bg-primary/20 text-primary px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50 disabled:cursor-wait"
          >
            {resolving ? 'Resolving…' : 'Mark resolved'}
          </button>
        )}
        {issue.slug && (
          <a
            href={`/wiki/${encodeURIComponent(issue.slug)}`}
            className="inline-flex items-center gap-1 rounded-md bg-muted hover:bg-muted/80 text-muted-foreground px-3 py-1.5 text-xs font-medium transition-colors"
          >
            Open page
            <ExternalLink size={10} />
          </a>
        )}
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">
          {new Date(issue.created_at).toLocaleDateString()}
        </span>
      </div>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
      <Wrench size={40} className="text-muted-foreground/40" />
      <p className="text-sm font-medium text-muted-foreground">All clean — no maintenance issues.</p>
      <p className="text-xs text-muted-foreground/70">The wiki maintenance scheduler will populate this queue when issues are detected.</p>
    </div>
  );
}

function PanelSkeleton() {
  return (
    <div className="p-6 space-y-4">
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
