import { useCallback, useState } from 'react';
import { toast } from 'sonner';
import { Check, X, ShieldQuestion } from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { PendingUserSummary } from '@/types/api';

const POLL_MS = 8000;

export function PendingUsersPanel() {
  const { t, formatDate } = useLocale();
  const fetcher = useCallback(() => api.pendingUsers(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher, POLL_MS);
  const [busy, setBusy] = useState<Set<string>>(new Set());

  const setBusyFor = useCallback((id: string, on: boolean) => {
    setBusy((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);

  const handleApprove = useCallback(async (p: PendingUserSummary) => {
    setBusyFor(p.user_id, true);
    const tid = toast.loading(t('pending.toast.approving', { user: p.username || p.user_id }));
    try {
      await api.approvePendingUser(p.user_id);
      toast.success(t('pending.toast.approved', { user: p.username || p.user_id }), { id: tid });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t('pending.toast.approveFailed', { msg }), { id: tid });
    } finally {
      setBusyFor(p.user_id, false);
    }
  }, [refetch, setBusyFor, t]);

  const handleDeny = useCallback(async (p: PendingUserSummary) => {
    setBusyFor(p.user_id, true);
    const tid = toast.loading(t('pending.toast.denying', { user: p.username || p.user_id }));
    try {
      await api.denyPendingUser(p.user_id);
      toast.success(t('pending.toast.denied', { user: p.username || p.user_id }), { id: tid });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t('pending.toast.denyFailed', { msg }), { id: tid });
    } finally {
      setBusyFor(p.user_id, false);
    }
  }, [refetch, setBusyFor, t]);

  if (loading && !data) return <PendingSkeleton />;
  if (error && !data) return <div className="p-6 text-sm text-destructive">{t('pending.error', { message: error.message })}</div>;
  if (!data) return null;

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">{t('pending.title')}</h1>
          {stale && (
            <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
              {t('common.stale')}
            </span>
          )}
        </div>
        <p className="text-xs text-muted-foreground" dangerouslySetInnerHTML={{ __html: t('pending.subtitle') }} />
      </header>

      {data.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 flex flex-col items-center gap-2 text-muted-foreground">
          <ShieldQuestion size={32} className="opacity-40" />
          <p className="text-sm font-medium">{t('pending.noRequests')}</p>
          <p className="text-xs text-center max-w-xs" dangerouslySetInnerHTML={{ __html: t('pending.emptyHint') }} />
        </div>
      ) : (
        <div className="rounded-lg border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
              <tr>
                <th className="text-left py-2 px-3 font-medium">{t('pending.table.username')}</th>
                <th className="text-left py-2 px-3 font-medium">{t('pending.table.telegramId')}</th>
                <th className="text-left py-2 px-3 font-medium">{t('pending.table.requested')}</th>
                <th className="text-right py-2 px-3 font-medium">{t('pending.table.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {data.map((p) => (
                <tr key={p.user_id} className="border-t hover:bg-muted/30">
                  <td className="py-2 px-3">
                    {p.username ? <span>@{p.username}</span> : <span className="text-muted-foreground italic">{t('pending.noUsername')}</span>}
                  </td>
                  <td className="py-2 px-3 font-mono text-xs">{p.user_id}</td>
                  <td className="py-2 px-3 text-xs text-muted-foreground">{formatDate(p.requested_at)}</td>
                  <td className="py-2 px-3 text-right space-x-1">
                    <button
                      type="button"
                      disabled={busy.has(p.user_id)}
                      onClick={() => void handleApprove(p)}
                      className="inline-flex min-h-11 items-center gap-1 rounded-md border border-emerald-500/50 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 hover:bg-emerald-500/20 disabled:opacity-50 disabled:cursor-wait dark:text-emerald-400"
                      title={t('pending.approveHint')}
                    >
                      <Check size={14} />
                      {t('pending.approve')}
                    </button>
                    <button
                      type="button"
                      disabled={busy.has(p.user_id)}
                      onClick={() => void handleDeny(p)}
                      className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
                      title={t('pending.denyHint')}
                    >
                      <X size={14} />
                      {t('pending.deny')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function PendingSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-56" />
      <Skeleton className="h-32 w-full" />
    </div>
  );
}
