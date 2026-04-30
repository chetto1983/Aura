import { useCallback, useState } from 'react';
import { toast } from 'sonner';
import { Check, X, ShieldQuestion } from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { PendingUserSummary } from '@/types/api';

const POLL_MS = 8000;

function shortDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

export function PendingUsersPanel() {
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
    const tid = toast.loading(`Approving @${p.username || p.user_id}…`);
    try {
      await api.approvePendingUser(p.user_id);
      toast.success(`Approved. A login token was sent over Telegram to @${p.username || p.user_id}.`, { id: tid });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Approve failed: ${msg}`, { id: tid });
    } finally {
      setBusyFor(p.user_id, false);
    }
  }, [refetch, setBusyFor]);

  const handleDeny = useCallback(async (p: PendingUserSummary) => {
    setBusyFor(p.user_id, true);
    const tid = toast.loading(`Denying @${p.username || p.user_id}…`);
    try {
      await api.denyPendingUser(p.user_id);
      toast.success(`Denied @${p.username || p.user_id}.`, { id: tid });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Deny failed: ${msg}`, { id: tid });
    } finally {
      setBusyFor(p.user_id, false);
    }
  }, [refetch, setBusyFor]);

  if (loading && !data) return <PendingSkeleton />;
  if (error && !data) return <div className="p-6 text-sm text-destructive">Error: {error.message}</div>;
  if (!data) return null;

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">Pending requests</h1>
          {stale && (
            <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
              ⚠ stale
            </span>
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          Telegram users who ran <code>/start</code> and need approval before they can use Aura.
        </p>
      </header>

      {data.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 flex flex-col items-center gap-2 text-muted-foreground">
          <ShieldQuestion size={32} className="opacity-40" />
          <p className="text-sm font-medium">No pending requests</p>
          <p className="text-xs text-center max-w-xs">
            When a stranger runs <code>/start</code>, you'll get a Telegram notification and the request will appear here for approval.
          </p>
        </div>
      ) : (
        <div className="rounded-lg border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
              <tr>
                <th className="text-left py-2 px-3 font-medium">Username</th>
                <th className="text-left py-2 px-3 font-medium">Telegram ID</th>
                <th className="text-left py-2 px-3 font-medium">Requested</th>
                <th className="text-right py-2 px-3 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {data.map((p) => (
                <tr key={p.user_id} className="border-t hover:bg-muted/30">
                  <td className="py-2 px-3">
                    {p.username ? <span>@{p.username}</span> : <span className="text-muted-foreground italic">(no username)</span>}
                  </td>
                  <td className="py-2 px-3 font-mono text-xs">{p.user_id}</td>
                  <td className="py-2 px-3 text-xs text-muted-foreground">{shortDate(p.requested_at)}</td>
                  <td className="py-2 px-3 text-right space-x-1">
                    <button
                      type="button"
                      disabled={busy.has(p.user_id)}
                      onClick={() => void handleApprove(p)}
                      className="inline-flex items-center gap-1 rounded-md border border-emerald-500/50 bg-emerald-500/10 px-2 py-1 text-xs text-emerald-700 hover:bg-emerald-500/20 disabled:opacity-50 disabled:cursor-wait dark:text-emerald-400"
                      title="Approve and send a Telegram login token"
                    >
                      <Check size={12} />
                      Approve
                    </button>
                    <button
                      type="button"
                      disabled={busy.has(p.user_id)}
                      onClick={() => void handleDeny(p)}
                      className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
                      title="Deny this request"
                    >
                      <X size={12} />
                      Deny
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
