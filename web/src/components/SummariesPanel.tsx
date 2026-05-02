import { useCallback, useState } from 'react';
import { FileCheck, ExternalLink } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { ProposedUpdate } from '@/types/api';

const ACTION_BADGE: Record<string, string> = {
  new: 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400',
  patch: 'bg-amber-500/15 text-amber-600 dark:text-amber-400',
  skip: 'bg-muted text-muted-foreground',
};

export function SummariesPanel() {
  const fetcher = useCallback(() => api.summaries('pending'), []);
  const { data, error, loading, refetch } = useApi<ProposedUpdate[]>(fetcher);
  const [dismissed, setDismissed] = useState<Set<number>>(new Set());

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Failed to load summaries" onRetry={refetch} />;

  const items = (data ?? []).filter((u) => !dismissed.has(u.id));

  return (
    <div className="p-6 space-y-4">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Summaries</h1>
        <p className="text-xs text-muted-foreground mt-0.5">Proposed wiki updates from auto-summarizer</p>
      </header>

      {items.length === 0 ? (
        <EmptyState />
      ) : (
        <div className="space-y-3">
          {items.map((update) => (
            <ProposalCard
              key={update.id}
              update={update}
              onDismiss={() => setDismissed((prev) => new Set(prev).add(update.id))}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ProposalCard({ update, onDismiss }: { update: ProposedUpdate; onDismiss: () => void }) {
  const [acting, setActing] = useState<'approve' | 'reject' | null>(null);

  const firstTurnId = update.source_turn_ids?.[0];

  const handleApprove = async () => {
    if (acting) return;
    setActing('approve');
    const tid = toast.loading('Approving…');
    try {
      await api.approveSummary(update.id);
      toast.success('Approved — wiki updated.', { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to approve', { id: tid });
      setActing(null);
    }
  };

  const handleReject = async () => {
    if (acting) return;
    setActing('reject');
    const tid = toast.loading('Rejecting…');
    try {
      await api.rejectSummary(update.id);
      toast.success('Rejected.', { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to reject', { id: tid });
      setActing(null);
    }
  };

  return (
    <div className="rounded-xl border bg-card p-5 space-y-3">
      <div className="flex items-start justify-between gap-3">
        <p className="text-sm leading-relaxed flex-1">{update.fact}</p>
        <span className={`shrink-0 inline-flex rounded-full px-2.5 py-0.5 text-xs font-medium ${ACTION_BADGE[update.action] ?? 'bg-muted text-muted-foreground'}`}>
          {update.action}
        </span>
      </div>

      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span>
          Target:{' '}
          {update.target_slug ? (
            <a
              href={`/wiki/${encodeURIComponent(update.target_slug)}`}
              className="text-primary hover:underline inline-flex items-center gap-1"
            >
              {update.target_slug}
              <ExternalLink size={10} />
            </a>
          ) : (
            <span className="rounded-full bg-muted px-2 py-0.5">new page</span>
          )}
        </span>

        {firstTurnId !== undefined && (
          <span>
            Source:{' '}
            <a
              href={`/conversations`}
              onClick={(e) => { e.preventDefault(); window.location.href = `/conversations#turn-${firstTurnId}`; }}
              className="text-primary hover:underline inline-flex items-center gap-1"
            >
              turn #{firstTurnId}
              <ExternalLink size={10} />
            </a>
          </span>
        )}

        <span>
          Score:{' '}
          <span className="tabular-nums font-medium text-foreground">
            {update.similarity.toFixed(2)}
          </span>
        </span>

        <span className="ml-auto">{new Date(update.created_at).toLocaleDateString()}</span>
      </div>

      <div className="flex gap-2 pt-1">
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void handleApprove()}
          className="rounded-md bg-primary/10 hover:bg-primary/20 text-primary px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50 disabled:cursor-wait"
        >
          {acting === 'approve' ? 'Approving…' : 'Approve'}
        </button>
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void handleReject()}
          className="rounded-md bg-muted hover:bg-muted/80 text-muted-foreground px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50 disabled:cursor-wait"
        >
          {acting === 'reject' ? 'Rejecting…' : 'Reject'}
        </button>
      </div>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
      <FileCheck size={40} className="text-muted-foreground/40" />
      <p className="text-sm font-medium text-muted-foreground">No pending proposals</p>
      <p className="text-xs text-muted-foreground/70">
        Review mode disabled or no proposals yet. Set{' '}
        <code className="rounded bg-muted px-1 py-0.5 font-mono">SUMMARIZER_MODE=review</code>{' '}
        in .env to enable.
      </p>
    </div>
  );
}

function PanelSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-40" />
      {[0, 1, 2].map((i) => (
        <div key={i} className="rounded-xl border p-5 space-y-3">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
          <div className="flex gap-2">
            <Skeleton className="h-7 w-20" />
            <Skeleton className="h-7 w-16" />
          </div>
        </div>
      ))}
    </div>
  );
}
