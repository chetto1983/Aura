import { useCallback, useMemo, useState } from 'react';
import { Archive, Boxes, Database, FileArchive, FileText, HardDriveUpload, RotateCw, ShieldCheck } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import { ErrorCard } from '@/components/common/ErrorCard';
import { Skeleton } from '@/components/ui/skeleton';
import type { BackupObject } from '@/types/api';

const CATEGORY_ICON: Record<string, typeof Archive> = {
  full_restore: Archive,
  source_originals: Boxes,
  extractions: FileText,
  memory_snapshot: ShieldCheck,
  embedding_index: Database,
  audit_bundle: FileArchive,
  manifest: FileText,
  artifact: FileArchive,
};

const CATEGORY_ORDER = [
  'full_restore',
  'source_originals',
  'extractions',
  'memory_snapshot',
  'embedding_index',
  'audit_bundle',
  'manifest',
  'artifact',
];

export function BackupsPanel() {
  const { t, formatDate, formatNumber } = useLocale();
  const fetcher = useCallback(() => api.backups(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher);
  const [exporting, setExporting] = useState(false);

  const latestTimestamp = data?.objects.find((obj) => obj.timestamp)?.timestamp;
  const latestObjects = useMemo(
    () => latestTimestamp ? data?.objects.filter((obj) => obj.timestamp === latestTimestamp) ?? [] : [],
    [data?.objects, latestTimestamp],
  );
  const grouped = useMemo(() => groupByTimestamp(data?.objects ?? []), [data?.objects]);
  const totalBytes = (data?.objects ?? []).reduce((sum, obj) => sum + obj.size_bytes, 0);

  const handleExport = async () => {
    if (exporting) return;
    setExporting(true);
    const id = toast.loading(t('backups.toast.exporting'));
    try {
      const res = await api.exportBackups();
      toast.success(t('backups.toast.exported', { count: String(res.objects.length) }), { id });
      await refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('backups.toast.exportFailed'), { id });
    } finally {
      setExporting(false);
    }
  };

  if (loading && !data) return <BackupsSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('backups.errorTitle')} onRetry={refetch} />;

  return (
    <div className="p-6 space-y-5">
      <header className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('backups.title')}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('backups.subtitle', { bucket: data?.bucket || 'aura-artifacts' })}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {stale && <span className="rounded-full bg-amber-500/10 px-2 py-1 text-xs text-amber-600 dark:text-amber-400">{t('common.stale')}</span>}
          <button
            type="button"
            onClick={() => void refetch()}
            className="inline-flex min-h-11 items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted"
            title={t('backups.refreshHint')}
          >
            <RotateCw size={16} />
            {t('backups.refresh')}
          </button>
          <button
            type="button"
            disabled={exporting}
            onClick={() => void handleExport()}
            className="inline-flex min-h-11 items-center gap-2 rounded-md bg-primary/10 px-3 py-2 text-sm font-medium text-primary hover:bg-primary/20 disabled:cursor-wait disabled:opacity-50"
            title={t('backups.exportHint')}
          >
            <HardDriveUpload size={16} />
            {exporting ? t('backups.exporting') : t('backups.exportNow')}
          </button>
        </div>
      </header>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <Metric label={t('backups.metric.objects')} value={formatNumber(data?.objects.length ?? 0)} />
        <Metric label={t('backups.metric.totalSize')} value={formatBytes(totalBytes, formatNumber)} />
        <Metric label={t('backups.metric.latest')} value={latestTimestamp ? formatTimestamp(latestTimestamp) : t('backups.none')} />
      </div>

      {latestObjects.length > 0 && (
        <section className="rounded-lg border bg-card">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold">{t('backups.latestSet')}</h2>
              <p className="text-xs text-muted-foreground">{formatTimestamp(latestTimestamp ?? '')}</p>
            </div>
            <span className="rounded-md bg-primary/10 px-2 py-1 text-xs font-medium text-primary">
              {t('backups.objectCount', { count: String(latestObjects.length) })}
            </span>
          </div>
          <div className="grid gap-2 p-3 md:grid-cols-2 xl:grid-cols-3">
            {CATEGORY_ORDER.map((category) => latestObjects.find((obj) => obj.category === category)).filter(Boolean).map((obj) => (
              <ArtifactTile key={obj!.key} obj={obj!} />
            ))}
          </div>
        </section>
      )}

      <section className="rounded-lg border bg-card">
        <div className="border-b px-4 py-3">
          <h2 className="text-sm font-semibold">{t('backups.history')}</h2>
        </div>
        {grouped.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground">{t('backups.empty')}</div>
        ) : (
          <div className="divide-y">
            {grouped.map((group) => (
              <div key={group.timestamp} className="grid gap-3 px-4 py-3 md:grid-cols-[170px_1fr_auto] md:items-center">
                <div>
                  <div className="font-mono text-sm">{formatTimestamp(group.timestamp)}</div>
                  <div className="text-xs text-muted-foreground">{formatDate(group.newest.toISOString(), { dateStyle: 'short', timeStyle: 'short' })}</div>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {CATEGORY_ORDER.map((category) => {
                    const count = group.objects.filter((obj) => obj.category === category).length;
                    if (!count) return null;
                    return <CategoryPill key={category} category={category} count={count} />;
                  })}
                </div>
                <div className="text-sm tabular-nums text-muted-foreground">{formatBytes(group.bytes, formatNumber)}</div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="mt-2 text-2xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function ArtifactTile({ obj }: { obj: BackupObject }) {
  const { t, formatNumber } = useLocale();
  const Icon = CATEGORY_ICON[obj.category] ?? FileArchive;
  return (
    <div className="rounded-md border bg-background p-3">
      <div className="flex items-start gap-3">
        <span className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
          <Icon size={17} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{t(`backups.category.${obj.category}`)}</div>
          <div className="mt-1 truncate font-mono text-xs text-muted-foreground" title={obj.key}>{obj.key}</div>
          <div className="mt-2 text-xs tabular-nums text-muted-foreground">{formatBytes(obj.size_bytes, formatNumber)}</div>
        </div>
      </div>
    </div>
  );
}

function CategoryPill({ category, count }: { category: string; count: number }) {
  const { t } = useLocale();
  const Icon = CATEGORY_ICON[category] ?? FileArchive;
  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-1 text-xs">
      <Icon size={12} />
      {t(`backups.category.${category}`)}
      {count > 1 && <span className="tabular-nums text-muted-foreground">{count}</span>}
    </span>
  );
}

function BackupsSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-44" />
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        {[0, 1, 2].map((i) => <Skeleton key={i} className="h-24 rounded-lg" />)}
      </div>
      <Skeleton className="h-64 rounded-lg" />
    </div>
  );
}

function groupByTimestamp(objects: BackupObject[]) {
  const groups = new Map<string, { timestamp: string; objects: BackupObject[]; bytes: number; newest: Date }>();
  for (const obj of objects) {
    const timestamp = obj.timestamp || 'legacy';
    const group = groups.get(timestamp) ?? { timestamp, objects: [], bytes: 0, newest: new Date(0) };
    group.objects.push(obj);
    group.bytes += obj.size_bytes;
    const modified = new Date(obj.last_modified);
    if (modified > group.newest) group.newest = modified;
    groups.set(timestamp, group);
  }
  return Array.from(groups.values()).sort((a, b) => b.newest.getTime() - a.newest.getTime());
}

function formatTimestamp(timestamp: string): string {
  if (!timestamp || timestamp === 'legacy') return timestamp || '';
  return `${timestamp.slice(0, 10)} ${timestamp.slice(11, 13)}:${timestamp.slice(13, 15)}:${timestamp.slice(15, 17)}`;
}

function formatBytes(bytes: number, formatNumber: (n: number, options?: Intl.NumberFormatOptions) => string): string {
  if (bytes < 1024) return `${formatNumber(bytes)} B`;
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  let value = bytes / 1024;
  let unit = units[0];
  for (let i = 1; value >= 1024 && i < units.length; i++) {
    value /= 1024;
    unit = units[i];
  }
  return `${formatNumber(value, { maximumFractionDigits: value >= 10 ? 1 : 2 })} ${unit}`;
}
