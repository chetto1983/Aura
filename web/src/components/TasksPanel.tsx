import { useCallback, useEffect, useState } from 'react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { Task } from '@/types/api';

const POLL_MS = 5000;
const STATUS_ORDER: Task['status'][] = ['active', 'done', 'cancelled', 'failed'];
const STATUS_LABEL: Record<Task['status'], string> = {
  active: '🔴 Active',
  done: '✅ Done',
  cancelled: '⏸ Cancelled',
  failed: '❌ Failed',
};

export function TasksPanel() {
  const fetcher = useCallback(() => api.tasks(), []);
  const { data, error, loading, stale } = useApi(fetcher, POLL_MS);

  if (loading && !data) return <div className="p-6 text-sm text-muted-foreground">Loading…</div>;
  if (error && !data) return <div className="p-6 text-sm text-destructive">Error: {error.message}</div>;
  if (!data) return null;

  const grouped: Record<string, Task[]> = {};
  for (const t of data) (grouped[t.status] ??= []).push(t);

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Scheduled tasks</h1>
        {stale && (
          <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
            ⚠ stale
          </span>
        )}
      </header>

      {data.length === 0 && (
        <p className="text-sm text-muted-foreground">No scheduled tasks.</p>
      )}

      {STATUS_ORDER.map((s) => {
        const rows = grouped[s];
        if (!rows || rows.length === 0) return null;
        return (
          <section key={s}>
            <h2 className="text-sm font-medium text-muted-foreground mb-2">
              {STATUS_LABEL[s]} <span className="ml-1 tabular-nums">({rows.length})</span>
            </h2>
            <div className="rounded-lg border overflow-hidden">
              <table className="w-full text-sm">
                <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                  <tr>
                    <th className="text-left py-2 px-3 font-medium">Name</th>
                    <th className="text-left py-2 px-3 font-medium">Kind</th>
                    <th className="text-left py-2 px-3 font-medium">Schedule</th>
                    <th className="text-left py-2 px-3 font-medium">Next run</th>
                    <th className="text-left py-2 px-3 font-medium">Last run / error</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((t) => (
                    <tr key={t.name} className="border-t hover:bg-muted/30 align-top">
                      <td className="py-2 px-3 font-mono text-xs">{t.name}</td>
                      <td className="py-2 px-3">{t.kind}</td>
                      <td className="py-2 px-3 text-xs">
                        {t.schedule_kind === 'daily' ? `daily ${t.schedule_daily}` : t.schedule_at}
                      </td>
                      <td className="py-2 px-3">
                        {t.status === 'active'
                          ? <Countdown iso={t.next_run_at} />
                          : <span className="text-xs text-muted-foreground">{shortDate(t.next_run_at)}</span>}
                      </td>
                      <td className="py-2 px-3 text-xs">
                        {t.last_error ? (
                          <span className="text-destructive">{t.last_error}</span>
                        ) : t.last_run_at ? (
                          <span className="text-muted-foreground">{shortDate(t.last_run_at)}</span>
                        ) : <span className="text-muted-foreground">—</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        );
      })}
    </div>
  );
}

function Countdown({ iso }: { iso: string }) {
  // Capture `now` in state — calling Date.now() during render violates the
  // react-hooks/purity rule because re-renders happen unpredictably.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const target = new Date(iso).getTime();
  const diff = Math.max(0, Math.round((target - now) / 1000));
  if (diff <= 0) return <span className="text-xs text-muted-foreground">due</span>;
  const h = Math.floor(diff / 3600);
  const m = Math.floor((diff % 3600) / 60);
  const s = diff % 60;
  return <span className="font-mono text-xs tabular-nums">{h}h {m}m {s}s</span>;
}

function shortDate(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  return new Date(iso).toLocaleString();
}
