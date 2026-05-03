import { useCallback, useMemo, useState } from 'react';
import { Activity, Bot, Clock3, Cpu, Database } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { ErrorCard } from '@/components/common/ErrorCard';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { SwarmRunDetail, SwarmRunSummary, SwarmTask } from '@/types/api';

const POLL_MS = 5000;

const STATUS_CLASS: Record<string, string> = {
  pending: 'bg-slate-500/10 text-slate-600 dark:text-slate-300',
  running: 'bg-sky-500/15 text-sky-600 dark:text-sky-400',
  completed: 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400',
  failed: 'bg-rose-500/15 text-rose-600 dark:text-rose-400',
};

export function SwarmPanel() {
  const { t } = useLocale();
  const fetchRuns = useCallback(() => api.swarmRuns(50), []);
  const { data: runs, error, loading, stale, refetch } = useApi(fetchRuns, POLL_MS);
  const [selectedID, setSelectedID] = useState<string>('');

  const effectiveSelectedID = useMemo(() => {
    if (!runs || runs.length === 0) return '';
    if (selectedID && runs.some((run) => run.id === selectedID)) return selectedID;
    return runs[0].id;
  }, [runs, selectedID]);
  const selected = useMemo(
    () => runs?.find((run) => run.id === effectiveSelectedID),
    [runs, effectiveSelectedID],
  );
  const fetchDetail = useCallback(
    () => effectiveSelectedID ? api.swarmRun(effectiveSelectedID) : Promise.resolve(undefined as unknown as SwarmRunDetail),
    [effectiveSelectedID],
  );
  const { data: detail, error: detailError, loading: detailLoading, refetch: refetchDetail } = useApi(fetchDetail, effectiveSelectedID ? POLL_MS : undefined);

  if (loading && !runs) return <PanelSkeleton />;
  if (error && !runs) return <ErrorCard error={error} title={t('swarm.errorTitle')} onRetry={refetch} />;

  const safeRuns = runs ?? [];

  return (
    <div className="p-6 space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-md bg-primary/10 text-primary">
            <Bot size={18} />
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">{t('swarm.title')}</h1>
            <p className="text-xs text-muted-foreground mt-0.5">{t('swarm.subtitle')}</p>
          </div>
          {stale && (
            <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
              {t('swarm.stale')}
            </span>
          )}
        </div>
      </header>

      {safeRuns.length === 0 ? (
        <EmptyState />
      ) : (
        <div className="grid gap-6 xl:grid-cols-[minmax(320px,420px)_1fr]">
          <RunList runs={safeRuns} selectedID={effectiveSelectedID} onSelect={setSelectedID} />
          <section className="min-w-0">
            {detailError && !detail ? (
              <ErrorCard error={detailError} title={t('swarm.errorDetailTitle')} onRetry={refetchDetail} />
            ) : detailLoading && !detail ? (
              <DetailSkeleton />
            ) : detail && selected ? (
              <RunDetail run={detail} />
            ) : null}
          </section>
        </div>
      )}
    </div>
  );
}

function RunList({
  runs,
  selectedID,
  onSelect,
}: {
  runs: SwarmRunSummary[];
  selectedID: string;
  onSelect: (id: string) => void;
}) {
  const { t } = useLocale();
  return (
    <section className="space-y-2">
      {runs.map((run) => (
        <button
          key={run.id}
          type="button"
          onClick={() => onSelect(run.id)}
          className={`w-full rounded-md border bg-card p-3 text-left transition-colors hover:bg-muted/50 ${
            selectedID === run.id ? 'border-primary/40 ring-1 ring-primary/20' : ''
          }`}
        >
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{run.goal}</p>
              <p className="mt-1 font-mono text-[11px] text-muted-foreground">{run.id}</p>
            </div>
            <StatusBadge status={run.status} />
          </div>
          <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-muted-foreground">
            <Metric label={t('swarm.metric.tasks')} value={run.task_counts.total.toString()} />
            <Metric label={t('swarm.metric.tokens')} value={compact(run.metrics.tokens_total)} />
            <Metric label={t('swarm.metric.speed')} value={`${run.metrics.speedup.toFixed(1)}x`} />
          </div>
        </button>
      ))}
    </section>
  );
}

function RunDetail({ run }: { run: SwarmRunDetail }) {
  const { t, formatDate } = useLocale();
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Stat icon={Activity} label={t('swarm.stat.tasks')} value={`${run.task_counts.completed}/${run.task_counts.total}`} />
        <Stat icon={Cpu} label={t('swarm.stat.llmCalls')} value={run.metrics.llm_calls.toString()} />
        <Stat icon={Database} label={t('swarm.stat.tokens')} value={compact(run.metrics.tokens_total)} />
        <Stat icon={Clock3} label={t('swarm.stat.speedup')} value={`${run.metrics.speedup.toFixed(2)}x`} />
      </div>

      <div className="rounded-md border bg-card">
        <div className="flex flex-wrap items-start justify-between gap-3 border-b p-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="truncate text-base font-semibold">{run.goal}</h2>
              <StatusBadge status={run.status} />
            </div>
            <p className="mt-1 font-mono text-xs text-muted-foreground">{run.id}</p>
          </div>
          <div className="text-right text-xs text-muted-foreground">
            <p>{formatDate(run.created_at, { dateStyle: 'short', timeStyle: 'medium' })}</p>
            {run.completed_at && <p>{run.metrics.wall_ms} {t('swarm.stat.wallMs')}</p>}
          </div>
        </div>
        {run.last_error && (
          <div className="border-b border-rose-500/20 bg-rose-500/10 p-3 text-sm text-rose-600 dark:text-rose-400">
            {run.last_error}
          </div>
        )}
        <div className="divide-y">
          {run.tasks.map((task) => (
            <TaskRow key={task.id} task={task} />
          ))}
        </div>
      </div>
    </div>
  );
}

function TaskRow({ task }: { task: SwarmTask }) {
  const { t } = useLocale();
  return (
    <article className="p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge status={task.status} />
            <span className="rounded bg-muted px-2 py-0.5 text-xs font-medium">{task.role}</span>
            <span className="text-xs text-muted-foreground">{t('swarm.task.depth')} {task.depth}</span>
          </div>
          <h3 className="mt-2 text-sm font-medium">{task.subject || task.id}</h3>
          <p className="mt-1 font-mono text-[11px] text-muted-foreground">{task.id}</p>
        </div>
        <div className="grid grid-cols-3 gap-2 text-right text-xs">
          <Metric label={t('swarm.task.llmCalls')} value={task.llm_calls.toString()} />
          <Metric label={t('swarm.task.toolCalls')} value={task.tool_calls.toString()} />
          <Metric label={t('swarm.task.elapsedMs')} value={task.elapsed_ms.toString()} />
        </div>
      </div>
      {task.tool_allowlist && task.tool_allowlist.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1">
          {task.tool_allowlist.map((tool) => (
            <span key={tool} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
              {tool}
            </span>
          ))}
        </div>
      )}
      {(task.result || task.last_error) && (
        <pre className={`mt-3 max-h-48 overflow-auto whitespace-pre-wrap rounded-md border p-3 text-xs ${
          task.last_error ? 'border-rose-500/20 bg-rose-500/10 text-rose-700 dark:text-rose-300' : 'bg-muted/40 text-muted-foreground'
        }`}>
          {task.last_error || task.result}
        </pre>
      )}
    </article>
  );
}

function Stat({ icon: Icon, label, value }: { icon: LucideIcon; label: string; value: string }) {
  return (
    <div className="rounded-md border bg-card p-4">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Icon size={14} />
        {label}
      </div>
      <p className="mt-2 text-2xl font-semibold tabular-nums">{value}</p>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <span>
      <span className="block font-mono text-foreground">{value}</span>
      <span className="uppercase tracking-wide">{label}</span>
    </span>
  );
}

function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium ${STATUS_CLASS[status] ?? 'bg-muted text-muted-foreground'}`}>
      {status}
    </span>
  );
}

function EmptyState() {
  const { t } = useLocale();
  return (
    <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
      <Bot size={36} className="mx-auto opacity-40" />
      <p className="mt-3 text-sm font-medium">{t('swarm.emptyTitle')}</p>
      <p className="mx-auto mt-1 max-w-sm text-xs">{t('swarm.emptyHint')}</p>
    </div>
  );
}

function PanelSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-10 w-56" />
      <div className="grid gap-6 xl:grid-cols-[minmax(320px,420px)_1fr]">
        <div className="space-y-2">
          {[0, 1, 2].map((i) => <Skeleton key={i} className="h-28 w-full" />)}
        </div>
        <DetailSkeleton />
      </div>
    </div>
  );
}

function DetailSkeleton() {
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-24 w-full" />)}
      </div>
      <Skeleton className="h-80 w-full" />
    </div>
  );
}

function compact(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}m`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return n.toString();
}
