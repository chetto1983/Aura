import { useCallback, useMemo, useState } from 'react';
import { Check, ShieldAlert, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/api';
import { ErrorCard } from '@/components/common/ErrorCard';
import { Skeleton } from '@/components/ui/skeleton';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { ProposedUpdate } from '@/types/api';

export function QuarantinePanel() {
  const { t } = useLocale();
  const fetcher = useCallback(() => api.quarantine(), []);
  const { data, error, loading, refetch } = useApi<ProposedUpdate[]>(fetcher);
  const [dismissed, setDismissed] = useState<Set<number>>(new Set());

  const items = useMemo(
    () => (data ?? []).filter((item) => !dismissed.has(item.id)),
    [data, dismissed],
  );

  const dismiss = (id: number) => {
    setDismissed((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });
  };

  if (loading && !data) return <QuarantineSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('quarantine.errorTitle')} onRetry={refetch} />;

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('quarantine.title')}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">{t('quarantine.subtitle')}</p>
        </div>
        <div className="rounded-md border px-3 py-2 text-sm font-medium tabular-nums">
          {items.length}
        </div>
      </header>

      {items.length === 0 ? (
        <div className="rounded-md border bg-card p-6 text-sm text-muted-foreground">
          <div className="flex items-center gap-2 text-foreground font-medium">
            <ShieldAlert size={16} />
            {t('quarantine.emptyTitle')}
          </div>
          <p className="mt-2 text-xs">{t('quarantine.emptyHint')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {items.map((item) => (
            <QuarantineCard key={item.id} item={item} onDismiss={() => dismiss(item.id)} />
          ))}
        </div>
      )}
    </div>
  );
}

function QuarantineCard({ item, onDismiss }: { item: ProposedUpdate; onDismiss: () => void }) {
  const { t, formatDate } = useLocale();
  const [acting, setActing] = useState<'approve' | 'delete' | null>(null);

  const approve = async () => {
    if (acting) return;
    setActing('approve');
    const tid = toast.loading(t('quarantine.toast.approving'));
    try {
      await api.approveQuarantine(item.id);
      toast.success(t('quarantine.toast.approved'), { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('quarantine.toast.approveFailed'), { id: tid });
      setActing(null);
    }
  };

  const remove = async () => {
    if (acting) return;
    setActing('delete');
    const tid = toast.loading(t('quarantine.toast.deleting'));
    try {
      await api.deleteQuarantine(item.id);
      toast.success(t('quarantine.toast.deleted'), { id: tid });
      onDismiss();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('quarantine.toast.deleteFailed'), { id: tid });
      setActing(null);
    }
  };

  return (
    <article className="rounded-md border bg-card p-5 space-y-3">
      <div className="flex items-start gap-3">
        <div className="mt-0.5 rounded-md bg-destructive/10 p-2 text-destructive">
          <ShieldAlert size={18} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className="rounded-full bg-muted px-2 py-0.5">{item.kind || 'operational_memory'}</span>
            {item.target_slug && <span>{item.target_slug}</span>}
            {item.category && <span>{item.category}</span>}
            <span>{formatDate(item.created_at, { dateStyle: 'short', timeStyle: 'short' })}</span>
          </div>
          <p className="mt-3 whitespace-pre-wrap break-words text-sm leading-relaxed">{item.fact}</p>
          {item.signature_hash && (
            <p className="mt-3 text-[11px] text-muted-foreground tabular-nums">
              {t('quarantine.signature')} {item.signature_hash}
            </p>
          )}
        </div>
      </div>

      <div className="flex flex-wrap justify-end gap-2">
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void approve()}
          className="min-h-11 inline-flex items-center gap-2 rounded-md bg-primary/10 px-3 py-2 text-sm font-medium text-primary hover:bg-primary/20 disabled:cursor-wait disabled:opacity-50"
        >
          <Check size={16} />
          {acting === 'approve' ? t('quarantine.approving') : t('quarantine.approve')}
        </button>
        <button
          type="button"
          disabled={acting !== null}
          onClick={() => void remove()}
          className="min-h-11 inline-flex items-center gap-2 rounded-md bg-muted px-3 py-2 text-sm font-medium text-muted-foreground hover:bg-muted/80 disabled:cursor-wait disabled:opacity-50"
        >
          <Trash2 size={16} />
          {acting === 'delete' ? t('quarantine.deleting') : t('quarantine.delete')}
        </button>
      </div>
    </article>
  );
}

function QuarantineSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-56" />
      <Skeleton className="h-32 w-full" />
      <Skeleton className="h-32 w-full" />
    </div>
  );
}

export default QuarantinePanel;
