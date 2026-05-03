import { useCallback, useState } from 'react';
import { FileCheck, ExternalLink } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { ProposedUpdate } from '@/types/api';

const ACTION_BADGE: Record<string, string> = {
  new: 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400',
  patch: 'bg-amber-500/15 text-amber-600 dark:text-amber-400',
  skip: 'bg-muted text-muted-foreground',
};

export function SummariesPanel() {
  const { t } = useLocale();
  const fetcher = useCallback(() => api.summaries('pending'), []);
  const { data, error, loading, refetch } = useApi<ProposedUpdate[]>(fetcher);
  const [dismissed, setDismissed] = useState<Set<number>>(new Set());

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('summaries.errorTitle')} onRetry={refetch} />;

  const items = (data ?? []).filter((u) => !dismissed.has(u.id));

  return (
    <div className="p-6 space-y-4">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">{t('summaries.title')}</h1>
        <p className="text-xs text-muted-foreground mt-0.5">{t('summaries.subtitle')}</p>
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
  const { t, formatDate } = useLocale();
  const [acting, setActing] = useState<'approve' | 'reject' | null>(null);

  const firstTurnId = update.source_turn_ids?.[0];

  const handleApprove = async () => {
    if (acting) return;
    setActing('approve');
    const tid = toast.loading(t('summaries.toast.approving'));
    try {
      await api.approveSummary(update.id);
      toast.success(t('summaries.toast.approved'), { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('summaries.toast.approveFailed'), { id: tid });
      setActing(null);
    }
  };

  const handleReject = async () => {
    if (acting) return;
    setActing('reject');
    const tid = toast.loading(t('summaries.toast.rejecting'));
    try {
      await api.rejectSummary(update.id);
      toast.success(t('summaries.toast.rejected'), { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('summaries.toast.rejectFailed'), { id: tid });
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
          {t('summaries.card.target')}{' '}
          {update.target_slug ? (
            <a
              href={`/wiki/${encodeURIComponent(update.target_slug)}`}
              className="text-primary hover:underline inline-flex items-center gap-1"
            >
              {update.target_slug}
              <ExternalLink size={10} />
            </a>
          ) : (
            <span className="rounded-full bg-muted px-2 py-0.5">{t('summaries.card.newPage')}</span>
          )}
        </span>

        {firstTurnId !== undefined && (
          <span>
            {t('summaries.card.source')}{' '}
            <a
              href={`/conversations`}
              onClick={(e) => { e.preventDefault(); window.location.href = `/conversations#turn-${firstTurnId}`; }}
              className="text-primary hover:underline inline-flex items-center gap-1"
            >
              {t('summaries.card.turnPrefix')}{firstTurnId}
              <ExternalLink size={10} />
            </a>
          </span>
        )}

        <span>
          {t('summaries.card.score')}{' '}
          <span className="tabular-nums font-medium text-foreground">
            {update.similarity.toFixed(2)}
          </span>
        </span>

        <span className="ml-auto">{formatDate(update.created_at, { dateStyle: 'short' })}</span>
      </div>

      <div className="flex gap-2 pt-1">
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void handleApprove()}
          className="min-h-11 rounded-md bg-primary/10 hover:bg-primary/20 text-primary px-3 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-wait"
        >
          {acting === 'approve' ? t('summaries.card.approving') : t('summaries.card.approve')}
        </button>
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void handleReject()}
          className="min-h-11 rounded-md bg-muted hover:bg-muted/80 text-muted-foreground px-3 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-wait"
        >
          {acting === 'reject' ? t('summaries.card.rejecting') : t('summaries.card.reject')}
        </button>
      </div>
    </div>
  );
}

function EmptyState() {
  const { t } = useLocale();
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
      <FileCheck size={40} className="text-muted-foreground/40" />
      <p className="text-sm font-medium text-muted-foreground">{t('summaries.emptyTitle')}</p>
      <p className="text-xs text-muted-foreground" dangerouslySetInnerHTML={{ __html: t('summaries.emptyHint') }} />
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
