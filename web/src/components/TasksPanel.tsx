import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Plus, X, Calendar, Trash2 } from 'lucide-react';
import { ErrorCard } from '@/components/common/ErrorCard';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { confirm as confirmModal } from '@/lib/confirmModal';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { Task, UpsertTaskRequest } from '@/types/api';

const POLL_MS = 5000;
const STATUS_ORDER: Task['status'][] = ['active', 'done', 'cancelled', 'failed'];
const WEEKDAYS = [
  ['mon', 'Mon'],
  ['tue', 'Tue'],
  ['wed', 'Wed'],
  ['thu', 'Thu'],
  ['fri', 'Fri'],
  ['sat', 'Sat'],
  ['sun', 'Sun'],
] as const;
const STATUS_LABEL: Record<Task['status'], string> = {
  active: '🔴 Active',
  done: '✅ Done',
  cancelled: '⏸ Cancelled',
  failed: '❌ Failed',
};

export function TasksPanel() {
  const fetcher = useCallback(() => api.tasks(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher, POLL_MS);
  const [busyNames, setBusyNames] = useState<Set<string>>(new Set());
  const [dialogOpen, setDialogOpen] = useState(false);

  const setBusy = useCallback((name: string, on: boolean) => {
    setBusyNames((prev) => {
      const next = new Set(prev);
      if (on) next.add(name);
      else next.delete(name);
      return next;
    });
  }, []);

  const handleCancel = useCallback(async (t: Task) => {
    setBusy(t.name, true);
    const id = toast.loading(`Cancelling ${t.name}…`);
    try {
      await api.cancelTask(t.name);
      toast.success(`Cancelled ${t.name}`, { id });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Cancel failed: ${t.name}\n${msg}`, { id });
    } finally {
      setBusy(t.name, false);
    }
  }, [refetch, setBusy]);

  const handleDelete = useCallback(async (t: Task) => {
    const ok = await confirmModal({
      title: `Delete task "${t.name}"?`,
      description: 'This removes the row entirely. Cancelling instead keeps the audit trail.',
      confirmLabel: 'Delete permanently',
      destructive: true,
    });
    if (!ok) return;
    setBusy(t.name, true);
    const id = toast.loading(`Deleting ${t.name}…`);
    try {
      await api.deleteTask(t.name);
      toast.success(`Deleted ${t.name}`, { id });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Delete failed: ${t.name}\n${msg}`, { id });
    } finally {
      setBusy(t.name, false);
    }
  }, [refetch, setBusy]);

  const handleCreate = useCallback(async (req: UpsertTaskRequest) => {
    const id = toast.loading(`Scheduling ${req.name}…`);
    try {
      const saved = await api.upsertTask(req);
      toast.success(`Scheduled ${saved.name} (${saved.next_run_at})`, { id });
      refetch();
      setDialogOpen(false);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Schedule failed: ${msg}`, { id });
      // Keep the dialog open so the user can fix the input.
    }
  }, [refetch]);

  if (loading && !data) return <TasksSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Failed to load tasks" onRetry={refetch} />;
  if (!data) return null;

  const grouped: Record<string, Task[]> = {};
  for (const t of data) (grouped[t.status] ??= []).push(t);

  return (
    <div className="p-6 space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">Scheduled tasks</h1>
          {stale && (
            <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
              ⚠ stale
            </span>
          )}
        </div>
        <button
          type="button"
          onClick={() => setDialogOpen(true)}
          className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted"
        >
          <Plus size={14} />
          New task
        </button>
      </header>

      {data.length === 0 && (
        <div className="rounded-lg border border-dashed p-12 flex flex-col items-center gap-2 text-muted-foreground">
          <Calendar size={32} className="opacity-40" />
          <p className="text-sm font-medium">No scheduled tasks yet</p>
          <p className="text-xs text-center max-w-xs">
            Use &quot;+ New task&quot; above for a one-time reminder or daily wiki
            maintenance, or ask the bot to schedule one in chat.
          </p>
        </div>
      )}

      <NewTaskDialog open={dialogOpen} onOpenChange={setDialogOpen} onSubmit={handleCreate} />

      {STATUS_ORDER.map((s) => {
        const rows = grouped[s];
        if (!rows || rows.length === 0) return null;
        return (
          <section key={s}>
            <h2 className="text-sm font-medium text-muted-foreground mb-2">
              {STATUS_LABEL[s]} <span className="ml-1 tabular-nums">({rows.length})</span>
            </h2>
            <div className="space-y-2 md:hidden">
              {rows.map((t) => (
                <article key={t.name} className="rounded-lg border bg-card p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="break-words font-mono text-xs font-medium">{t.name}</p>
                      <p className="mt-1 text-sm">{t.kind}</p>
                    </div>
                    <span className="shrink-0 rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">{t.status}</span>
                  </div>
                  <div className="mt-3 grid gap-1 text-xs text-muted-foreground">
                    <p>Schedule: {formatSchedule(t)}</p>
                    <p>Next: {t.status === 'active' ? <Countdown iso={t.next_run_at} /> : shortDate(t.next_run_at)}</p>
                    <p>Last: {t.last_error || shortDate(t.last_run_at)}</p>
                  </div>
                  <TaskActions
                    task={t}
                    busy={busyNames.has(t.name)}
                    onCancel={() => void handleCancel(t)}
                    onDelete={() => void handleDelete(t)}
                  />
                </article>
              ))}
            </div>

            <div className="hidden rounded-lg border overflow-x-auto md:block">
              <table className="w-full text-sm min-w-[700px]">
                <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                  <tr>
                    <th className="text-left py-2 px-3 font-medium">Name</th>
                    <th className="text-left py-2 px-3 font-medium">Kind</th>
                    <th className="text-left py-2 px-3 font-medium">Schedule</th>
                    <th className="text-left py-2 px-3 font-medium">Next run</th>
                    <th className="text-left py-2 px-3 font-medium">Last run / error</th>
                    <th className="text-right py-2 px-3 font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((t) => (
                    <tr key={t.name} className="border-t hover:bg-muted/30 align-top">
                      <td className="py-2 px-3 font-mono text-xs">{t.name}</td>
                      <td className="py-2 px-3">{t.kind}</td>
                      <td className="py-2 px-3 text-xs">
                        {t.schedule_kind === 'daily' && formatSchedule(t)}
                        {t.schedule_kind === 'every' && `every ${t.schedule_every_minutes}m`}
                        {t.schedule_kind === 'at' && t.schedule_at}
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
                      <td className="py-2 px-3 text-right">
                        <TaskActions
                          task={t}
                          busy={busyNames.has(t.name)}
                          onCancel={() => void handleCancel(t)}
                          onDelete={() => void handleDelete(t)}
                        />
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

// NewTaskDialog is a minimal "+ New task" form. Mirrors the schedule_task
// LLM tool: a name, a kind (reminder | wiki_maintenance), payload, and one
// of `at` (RFC3339 UTC) or `daily` (HH:MM in the bot's local TZ).
//
// Reminders require a recipient_id; the field is shown only when kind is
// reminder. The form submits a UpsertTaskRequest unchanged — server-side
// validation surfaces clear messages back through the toast.
function NewTaskDialog({
  open,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  onSubmit: (req: UpsertTaskRequest) => Promise<void>;
}) {
  // Keying the form on `open` means each open mounts a fresh form with
  // default useState values — no setState-in-effect dance to clear state.
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New scheduled task</DialogTitle>
          <DialogDescription>
            One-time (specific date/time) or daily recurring. Names must be unique;
            scheduling a name that already exists overwrites it.
          </DialogDescription>
        </DialogHeader>
        {open && <NewTaskForm key={String(open)} onCancel={() => onOpenChange(false)} onSubmit={onSubmit} />}
      </DialogContent>
    </Dialog>
  );
}

function TaskActions({
  task,
  busy,
  onCancel,
  onDelete,
}: {
  task: Task;
  busy: boolean;
  onCancel: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="mt-3 inline-flex flex-wrap items-center justify-end gap-1.5 md:mt-0">
      {task.status === 'active' && (
        <button
          type="button"
          disabled={busy}
          onClick={onCancel}
          className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
          title="Mark cancelled (keeps audit trail)"
        >
          <X size={14} />
          Cancel
        </button>
      )}
      <button
        type="button"
        disabled={busy}
        onClick={onDelete}
        className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm text-destructive hover:bg-destructive/10 disabled:opacity-50 disabled:cursor-wait"
        title="Permanently remove the row"
      >
        <Trash2 size={14} />
        Delete
      </button>
    </div>
  );
}

function formatSchedule(t: Task): string {
  if (t.schedule_kind === 'daily') {
    const days = t.schedule_weekdays ? ` on ${t.schedule_weekdays}` : '';
    return `daily ${t.schedule_daily}${days}`;
  }
  if (t.schedule_kind === 'every') return `every ${t.schedule_every_minutes}m`;
  return t.schedule_at || 'not scheduled';
}

function NewTaskForm({
  onCancel,
  onSubmit,
}: {
  onCancel: () => void;
  onSubmit: (req: UpsertTaskRequest) => Promise<void>;
}) {
  const [name, setName] = useState('');
  const [kind, setKind] = useState<Task['kind']>('wiki_maintenance');
  const [payload, setPayload] = useState('');
  const [recipientId, setRecipientId] = useState('');
  const [scheduleMode, setScheduleMode] = useState<'at' | 'daily' | 'every'>('daily');
  const [at, setAt] = useState('');
  const [daily, setDaily] = useState('03:00');
  const [weekdays, setWeekdays] = useState<string[]>([]);
  const [everyMinutes, setEveryMinutes] = useState(60);
  const [submitting, setSubmitting] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      const req: UpsertTaskRequest = { name: name.trim(), kind };
      if (payload.trim()) req.payload = payload.trim();
      if (kind === 'reminder' && recipientId.trim()) req.recipient_id = recipientId.trim();
      if (scheduleMode === 'at') {
        // <input type="datetime-local"> emits "YYYY-MM-DDTHH:MM" in local
        // time. Convert to UTC RFC3339 for the wire.
        const localDate = new Date(at);
        if (!isNaN(localDate.getTime())) {
          req.at = localDate.toISOString();
        }
      } else if (scheduleMode === 'daily') {
        req.daily = daily.trim();
        if (weekdays.length > 0) req.weekdays = weekdays.join(',');
      } else {
        req.every_minutes = Math.max(1, Math.floor(everyMinutes));
      }
      await onSubmit(req);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={(e) => void submit(e)} className="space-y-3">
          <label className="block text-sm">
            Name <span className="text-destructive">*</span>
            <input
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              pattern="[A-Za-z0-9_.\-]{1,64}"
              placeholder="e.g. weekly-cleanup"
              autoComplete="off"
              spellCheck={false}
              className="mt-1 min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm"
            />
            <span className="mt-1 block text-xs text-muted-foreground">
              1-64 chars, letters / digits / _ . -
            </span>
          </label>

          <label className="block text-sm">
            Kind
            <select
              value={kind}
              onChange={(e) => setKind(e.target.value as Task['kind'])}
              className="mt-1 min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm"
            >
              <option value="wiki_maintenance">wiki_maintenance — runs lint + rebuild + log</option>
              <option value="reminder">reminder — sends a Telegram message</option>
            </select>
          </label>

          {kind === 'reminder' && (
            <label className="block text-sm">
              Recipient (Telegram user ID) <span className="text-destructive">*</span>
              <input
                type="text"
                required
                value={recipientId}
                onChange={(e) => setRecipientId(e.target.value)}
                inputMode="numeric"
                autoComplete="off"
                placeholder="e.g. 123456789"
                className="mt-1 min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm"
              />
              <span className="mt-1 block text-xs text-muted-foreground">
                Numeric ID — message @userinfobot on Telegram to find yours.
              </span>
            </label>
          )}

          <label className="block text-sm">
            Payload (optional)
            <input
              type="text"
              value={payload}
              onChange={(e) => setPayload(e.target.value)}
              placeholder={kind === 'reminder' ? 'reminder text' : ''}
              className="mt-1 min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm"
            />
          </label>

          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">Schedule</legend>
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              <label className="inline-flex min-h-11 items-center gap-2 text-sm">
                <input
                  type="radio"
                  name="schedule"
                  checked={scheduleMode === 'daily'}
                  onChange={() => setScheduleMode('daily')}
                />
                Daily at HH:MM
              </label>
              <label className="inline-flex min-h-11 items-center gap-2 text-sm">
                <input
                  type="radio"
                  name="schedule"
                  checked={scheduleMode === 'every'}
                  onChange={() => setScheduleMode('every')}
                />
                Every N minutes
              </label>
              <label className="inline-flex min-h-11 items-center gap-2 text-sm">
                <input
                  type="radio"
                  name="schedule"
                  checked={scheduleMode === 'at'}
                  onChange={() => setScheduleMode('at')}
                />
                Once at
              </label>
            </div>
            {scheduleMode === 'daily' && (
              <div className="space-y-2">
                <input
                  type="time"
                  required
                  value={daily}
                  onChange={(e) => setDaily(e.target.value)}
                  aria-label="Daily HH:MM"
                  className="block min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm"
                />
                <div className="grid grid-cols-4 gap-1 sm:grid-cols-7">
                  {WEEKDAYS.map(([value, label]) => (
                    <label
                      key={value}
                      className="inline-flex min-h-11 items-center justify-center gap-1 rounded-md border px-2 text-xs"
                    >
                      <input
                        type="checkbox"
                        checked={weekdays.includes(value)}
                        onChange={(e) => {
                          setWeekdays((prev) => e.target.checked
                            ? [...prev, value]
                            : prev.filter((day) => day !== value));
                        }}
                      />
                      {label}
                    </label>
                  ))}
                </div>
                <p className="text-xs text-muted-foreground">
                  Leave all days unchecked to run every day. Select Mon-Fri for business days.
                </p>
              </div>
            )}
            {scheduleMode === 'every' && (
              <div className="space-y-1">
                <input
                  type="number"
                  required
                  min={1}
                  value={everyMinutes}
                  onChange={(e) => setEveryMinutes(parseInt(e.target.value, 10) || 1)}
                  aria-label="Interval in minutes"
                  className="block min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm"
                />
                <p className="text-xs text-muted-foreground">
                  Examples: 60 = hourly · 1440 = daily · 10080 = weekly. First fire is N minutes from now.
                </p>
              </div>
            )}
            {scheduleMode === 'at' && (
              <input
                type="datetime-local"
                required
                value={at}
                onChange={(e) => setAt(e.target.value)}
                aria-label="Run at"
                className="block min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm"
              />
            )}
          </fieldset>

      <DialogFooter className="pt-2">
        <button
          type="button"
          onClick={onCancel}
          className="min-h-11 rounded-md border px-3 py-2 text-sm hover:bg-muted"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={submitting}
          className="min-h-11 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          {submitting ? 'Scheduling…' : 'Schedule'}
        </button>
      </DialogFooter>
    </form>
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

function TasksSkeleton() {
  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-9 w-28" />
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-32" />
        <div className="rounded-lg border overflow-hidden">
          {[0, 1, 2].map((i) => (
            <div key={i} className="border-t first:border-t-0 px-3 py-3 flex items-center gap-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-3 w-20" />
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-3 flex-1" />
              <Skeleton className="h-7 w-20" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
