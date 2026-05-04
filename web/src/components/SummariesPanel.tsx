import { useCallback, useMemo, useState } from 'react';
import { CheckCheck, ExternalLink, FileCheck, XCircle } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { ProposedUpdate, ProposalEvidenceRef } from '@/types/api';

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
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [batchAction, setBatchAction] = useState<'approve' | 'reject' | null>(null);

  const items = useMemo(
    () => (data ?? []).filter((u) => !dismissed.has(u.id)),
    [data, dismissed],
  );
  const selectedIds = items.filter((item) => selected.has(item.id)).map((item) => item.id);
  const allVisibleSelected = items.length > 0 && selectedIds.length === items.length;

  const dismissMany = (ids: number[]) => {
    setDismissed((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => next.add(id));
      return next;
    });
    setSelected((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => next.delete(id));
      return next;
    });
  };

  const toggleSelection = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleAllVisible = () => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allVisibleSelected) {
        items.forEach((item) => next.delete(item.id));
      } else {
        items.forEach((item) => next.add(item.id));
      }
      return next;
    });
  };

  const handleBatch = async (action: 'approve' | 'reject') => {
    if (selectedIds.length === 0 || batchAction) return;
    setBatchAction(action);
    const tid = toast.loading(action === 'approve' ? t('summaries.toast.approving') : t('summaries.toast.rejecting'));
    try {
      const resp = action === 'approve'
        ? await api.approveSummaries(selectedIds)
        : await api.rejectSummaries(selectedIds);
      const updatedIds = resp.updated.map((item) => item.id);
      dismissMany(updatedIds);
      if (resp.failed.length > 0) {
        toast.warning(`${t('summaries.toast.batchPartial')} ${resp.updated.length}/${selectedIds.length}`, { id: tid });
      } else {
        toast.success(action === 'approve' ? t('summaries.toast.batchApproved') : t('summaries.toast.batchRejected'), { id: tid });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('summaries.toast.batchFailed'), { id: tid });
    } finally {
      setBatchAction(null);
    }
  };

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('summaries.errorTitle')} onRetry={refetch} />;

  return (
    <div className="p-6 space-y-4">
      <header className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('summaries.title')}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">{t('summaries.subtitle')}</p>
        </div>

        {items.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={toggleAllVisible}
              className="min-h-11 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted transition-colors"
            >
              {allVisibleSelected ? t('summaries.batch.clear') : t('summaries.batch.selectAll')}
            </button>
            <button
              type="button"
              disabled={selectedIds.length === 0 || batchAction !== null}
              onClick={() => void handleBatch('approve')}
              className="min-h-11 rounded-md bg-primary/10 hover:bg-primary/20 text-primary px-3 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-wait inline-flex items-center gap-2"
            >
              <CheckCheck size={16} />
              {batchAction === 'approve' ? t('summaries.batch.approving') : `${t('summaries.batch.approve')} (${selectedIds.length})`}
            </button>
            <button
              type="button"
              disabled={selectedIds.length === 0 || batchAction !== null}
              onClick={() => void handleBatch('reject')}
              className="min-h-11 rounded-md bg-muted hover:bg-muted/80 text-muted-foreground px-3 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-wait inline-flex items-center gap-2"
            >
              <XCircle size={16} />
              {batchAction === 'reject' ? t('summaries.batch.rejecting') : `${t('summaries.batch.reject')} (${selectedIds.length})`}
            </button>
          </div>
        )}
      </header>

      {items.length === 0 ? (
        <EmptyState />
      ) : (
        <div className="space-y-3">
          {items.map((update) => (
            <ProposalCard
              key={update.id}
              update={update}
              selected={selected.has(update.id)}
              onToggleSelected={() => toggleSelection(update.id)}
              onDismiss={() => dismissMany([update.id])}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ProposalCard({
  update,
  selected,
  onToggleSelected,
  onDismiss,
}: {
  update: ProposedUpdate;
  selected: boolean;
  onToggleSelected: () => void;
  onDismiss: () => void;
}) {
  const { t, formatDate } = useLocale();
  const [acting, setActing] = useState<'approve' | 'reject' | null>(null);

  const firstTurnId = update.source_turn_ids?.[0];
  const provenance = update.provenance;
  const evidence = provenance?.evidence ?? [];

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
    <div className={`rounded-xl border bg-card p-5 space-y-3 ${selected ? 'ring-2 ring-primary/30' : ''}`}>
      <div className="flex items-start gap-3">
        <label className="min-h-11 min-w-11 -ml-2 -mt-2 inline-flex items-center justify-center rounded-md hover:bg-muted cursor-pointer">
          <input
            type="checkbox"
            checked={selected}
            onChange={onToggleSelected}
            className="h-4 w-4 accent-primary"
            aria-label={t('summaries.batch.selectOne')}
          />
        </label>
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

      {(provenance?.origin_tool || provenance?.origin_reason || evidence.length > 0) && (
        <div className="border-l-2 border-primary/30 pl-3 text-xs space-y-2">
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-muted-foreground">
            {provenance?.origin_tool && (
              <span>
                {t('summaries.provenance.origin')}{' '}
                <span className="font-medium text-foreground">{provenance.origin_tool}</span>
              </span>
            )}
            {provenance?.origin_reason && (
              <span className="max-w-full truncate">
                {t('summaries.provenance.reason')}{' '}
                <span className="text-foreground">{provenance.origin_reason}</span>
              </span>
            )}
          </div>
          {evidence.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {evidence.slice(0, 4).map((ref) => (
                <EvidenceChip key={`${ref.kind}:${ref.id}:${ref.page ?? 0}`} refItem={ref} />
              ))}
              {evidence.length > 4 && (
                <span className="rounded-full bg-muted px-2 py-1 text-muted-foreground">+{evidence.length - 4}</span>
              )}
            </div>
          )}
        </div>
      )}

      <div className="flex gap-2 pt-1">
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void handleApprove()}
          className="min-h-11 rounded-md bg-primary/10 hover:bg-primary/20 text-primary px-3 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-wait inline-flex items-center gap-2"
        >
          <CheckCheck size={16} />
          {acting === 'approve' ? t('summaries.card.approving') : t('summaries.card.approve')}
        </button>
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void handleReject()}
          className="min-h-11 rounded-md bg-muted hover:bg-muted/80 text-muted-foreground px-3 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-wait inline-flex items-center gap-2"
        >
          <XCircle size={16} />
          {acting === 'reject' ? t('summaries.card.rejecting') : t('summaries.card.reject')}
        </button>
      </div>
    </div>
  );
}

function EvidenceChip({ refItem }: { refItem: ProposalEvidenceRef }) {
  const label = `${refItem.kind}:${refItem.id}${refItem.page ? ` p.${refItem.page}` : ''}`;
  return (
    <span
      className="max-w-full rounded-full bg-muted px-2 py-1 text-muted-foreground truncate"
      title={[refItem.title, refItem.snippet].filter(Boolean).join(' - ') || label}
    >
      {label}
    </span>
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
