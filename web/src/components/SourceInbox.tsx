import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Upload, Play, RefreshCcw, Download } from 'lucide-react';
import { getToken } from '@/lib/auth';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import { ErrorCard } from '@/components/common/ErrorCard';
import { Skeleton } from '@/components/ui/skeleton';
import type { SourceSummary, UploadResponse } from '@/types/api';

const POLL_MS = 5000;
const STATUS_ORDER: SourceSummary['status'][] = ['failed', 'stored', 'ocr_complete', 'ingested'];

export function SourceInbox() {
  const { t, formatDate } = useLocale();
  const fetcher = useCallback(() => api.sources(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher, POLL_MS);
  const [uploading, setUploading] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const [focusedSourceId, setFocusedSourceId] = useState('');
  // Per-row in-flight tracking so the same button can't be double-clicked.
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());
  const fileInputRef = useRef<HTMLInputElement>(null);

  const statusLabel = (s: SourceSummary['status']): string => {
    switch (s) {
      case 'failed': return t('sources.status.failed');
      case 'stored': return t('sources.status.stored');
      case 'ocr_complete': return t('sources.status.ocrComplete');
      case 'ingested': return t('sources.status.ingested');
    }
  };

  const fmtDate = (iso: string): string => {
    if (!iso || iso.startsWith('0001')) return '—';
    return formatDate(iso, { dateStyle: 'short', timeStyle: 'short' });
  };

  const setBusy = useCallback((id: string, on: boolean) => {
    setBusyIds((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);

  useEffect(() => {
    const focusFromHash = () => {
      const prefix = '#source-';
      if (!window.location.hash.startsWith(prefix)) return;
      const id = decodeURIComponent(window.location.hash.slice(prefix.length));
      setFocusedSourceId(id);
      window.setTimeout(() => {
        const nodes = document.querySelectorAll<HTMLElement>(`[data-source-id="${id}"]`);
        const visible = Array.from(nodes).find((node) => node.getClientRects().length > 0);
        visible?.scrollIntoView({ block: 'center' });
      }, 0);
    };
    focusFromHash();
    window.addEventListener('hashchange', focusFromHash);
    return () => window.removeEventListener('hashchange', focusFromHash);
  }, []);

  const handleIngest = useCallback(async (s: SourceSummary) => {
    setBusy(s.id, true);
    const toastId = toast.loading(t('sources.toast.ingesting', { filename: s.filename }));
    try {
      const res = await api.ingestSource(s.id);
      toast.success(t('sources.toast.ingested', { filename: s.filename, note: res.note ?? 'ingested' }), { id: toastId });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`${t('sources.toast.ingestFailed', { filename: s.filename })}\n${msg}`, { id: toastId });
    } finally {
      setBusy(s.id, false);
    }
  }, [refetch, setBusy, t]);

  const handleReocr = useCallback(async (s: SourceSummary) => {
    setBusy(s.id, true);
    const toastId = toast.loading(t('sources.toast.reocring', { filename: s.filename }));
    try {
      const res = await api.reocrSource(s.id);
      toast.success(t('sources.toast.reocred', { filename: s.filename, note: res.note ?? 'OCR redone' }), { id: toastId });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`${t('sources.toast.reocrFailed', { filename: s.filename })}\n${msg}`, { id: toastId });
    } finally {
      setBusy(s.id, false);
    }
  }, [refetch, setBusy, t]);

  const handleFiles = useCallback(async (files: FileList | File[] | null) => {
    if (!files || files.length === 0) return;
    const list = Array.from(files);
    const pdfs = list.filter((f) => f.name.toLowerCase().endsWith('.pdf'));
    const skipped = list.length - pdfs.length;
    if (skipped > 0) {
      toast.warning(t('sources.toast.uploadSkipped', { count: skipped }));
    }
    if (pdfs.length === 0) return;

    setUploading(true);
    try {
      for (const f of pdfs) {
        const toastId = toast.loading(t('sources.toast.uploading', { filename: f.name }));
        try {
          const res: UploadResponse = await api.uploadSource(f);
          toast.success(formatUploadSummary(f.name, res, t), { id: toastId });
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err);
          toast.error(`${t('sources.toast.uploadFailed', { filename: f.name })}\n${msg}`, { id: toastId });
        }
        // Poll-friendly: each upload triggers a refresh so the table updates
        // without waiting for the 5s tick.
        refetch();
      }
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  }, [refetch, t]);

  if (loading && !data) return <SourceInboxSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('sources.errorTitle')} onRetry={refetch} />;
  if (!data) return null;

  const grouped: Record<string, SourceSummary[]> = {};
  for (const s of data ?? []) (grouped[s.status] ??= []).push(s);
  const isEmpty = (data?.length ?? 0) === 0;

  return (
    <div
      className="p-6 space-y-6"
      onDragOver={(e) => {
        if (Array.from(e.dataTransfer.types).includes('Files')) {
          e.preventDefault();
          setDragOver(true);
        }
      }}
      onDragLeave={(e) => {
        // Only flip off when leaving the outer container (relatedTarget null
        // or outside this element).
        if (!e.currentTarget.contains(e.relatedTarget as Node)) {
          setDragOver(false);
        }
      }}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
        void handleFiles(e.dataTransfer.files);
      }}
    >
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">{t('sources.title')}</h1>
        {stale && (
          <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
            {t('common.stale')}
          </span>
        )}
      </header>

      <DropZone
        active={dragOver}
        uploading={uploading}
        onPick={() => fileInputRef.current?.click()}
      />
      <input
        ref={fileInputRef}
        type="file"
        accept=".pdf,application/pdf"
        multiple
        className="hidden"
        onChange={(e) => void handleFiles(e.target.files)}
      />

      {isEmpty && (
        <p className="text-sm text-muted-foreground">
          {t('sources.emptyHint')}
        </p>
      )}

      {STATUS_ORDER.map((status) => {
        const rows = grouped[status];
        if (!rows || rows.length === 0) return null;
        return (
          <section key={status}>
            <h2 className="text-sm font-medium text-muted-foreground mb-2">
              {statusLabel(status)} <span className="ml-1 tabular-nums">({rows.length})</span>
            </h2>
            <div className="space-y-2 md:hidden">
              {rows.map((s) => (
                <article
                  key={s.id}
                  data-source-id={s.id}
                  className={`scroll-mt-24 rounded-lg border bg-card p-3 ${focusedSourceId === s.id ? 'ring-2 ring-primary/30' : ''}`}
                >
                  <p className="break-words font-mono text-xs font-medium">{s.filename}</p>
                  <div className="mt-2 grid gap-1 text-xs text-muted-foreground">
                    <p>{t('sources.mobile.created')} {fmtDate(s.created_at)}</p>
                    <p>{s.page_count ? t('sources.pageCount', { count: s.page_count }) : t('sources.pagesUnknown')}</p>
                  </div>
                  <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
                    <WikiRefs pages={s.wiki_pages} />
                    <SourceActions
                      source={s}
                      busy={busyIds.has(s.id)}
                      onIngest={() => void handleIngest(s)}
                      onReocr={() => void handleReocr(s)}
                    />
                  </div>
                </article>
              ))}
            </div>

            <div className="hidden rounded-lg border overflow-x-auto md:block">
              <table className="w-full text-sm min-w-[600px]">
                <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                  <tr>
                    <th className="text-left py-2 px-3 font-medium">{t('sources.table.filename')}</th>
                    <th className="text-left py-2 px-3 font-medium">{t('sources.table.created')}</th>
                    <th className="text-right py-2 px-3 font-medium">{t('sources.table.pages')}</th>
                    <th className="text-left py-2 px-3 font-medium">{t('sources.table.wiki')}</th>
                    <th className="text-right py-2 px-3 font-medium">{t('sources.table.actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((s) => (
                    <tr
                      key={s.id}
                      data-source-id={s.id}
                      className={`scroll-mt-24 border-t hover:bg-muted/30 ${focusedSourceId === s.id ? 'bg-primary/5' : ''}`}
                    >
                      <td className="py-2 px-3 font-mono text-xs">{s.filename}</td>
                      <td className="py-2 px-3 text-muted-foreground">{fmtDate(s.created_at)}</td>
                      <td className="py-2 px-3 text-right tabular-nums">{s.page_count ?? '—'}</td>
                      <td className="py-2 px-3">
                        {s.wiki_pages?.length
                          ? s.wiki_pages.map((p) => (
                              <code key={p} className="text-xs">[[{p}]]</code>
                            ))
                          : <span className="text-muted-foreground">{'—'}</span>}
                      </td>
                      <td className="py-2 px-3 text-right">
                        <SourceActions
                          source={s}
                          busy={busyIds.has(s.id)}
                          onIngest={() => void handleIngest(s)}
                          onReocr={() => void handleReocr(s)}
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

function DropZone({
  active,
  uploading,
  onPick,
}: {
  active: boolean;
  uploading: boolean;
  onPick: () => void;
}) {
  const { t } = useLocale();
  return (
    <button
      type="button"
      onClick={onPick}
      disabled={uploading}
      className={`w-full rounded-lg border-2 border-dashed px-6 py-8 text-center transition-colors ${
        active
          ? 'border-primary bg-primary/5'
          : 'border-muted-foreground/25 hover:border-muted-foreground/50'
      } ${uploading ? 'opacity-60 cursor-wait' : 'cursor-pointer'}`}
    >
      <Upload size={28} className="mx-auto mb-2 text-muted-foreground" />
      <p className="text-sm font-medium">
        {uploading ? t('sources.drop.uploading') : active ? t('sources.drop.active') : t('sources.drop.idle')}
      </p>
      <p className="mt-1 text-xs text-muted-foreground">
        {t('sources.drop.hint')}
      </p>
    </button>
  );
}

function WikiRefs({ pages }: { pages?: string[] }) {
  const { t } = useLocale();
  if (!pages?.length) {
    return <span className="text-xs text-muted-foreground">{t('sources.noWikiPage')}</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {pages.map((p) => (
        <code key={p} className="rounded bg-muted px-1.5 py-1 text-xs">[[{p}]]</code>
      ))}
    </div>
  );
}

// SourceActions renders the per-row Ingest / Re-OCR buttons. The set of
// available actions depends on the source's lifecycle status:
//   - stored:        OCR hasn't run; reocr button (re-runs Mistral OCR)
//   - failed:        OCR or ingest failed; both reocr + ingest available
//   - ocr_complete:  OCR done but ingest hasn't run; ingest button only
//   - ingested:      no actions (re-running ingest is a no-op; reocr is
//                    available only if the user explicitly wants to
//                    refresh the OCR — surface it via re-upload instead)
function SourceActions({
  source: s,
  busy,
  onIngest,
  onReocr,
}: {
  source: SourceSummary;
  busy: boolean;
  onIngest: () => void;
  onReocr: () => void;
}) {
  const { t } = useLocale();

  const handleDownload = async () => {
    const token = getToken();
    if (!token) {
      toast.error(t('sources.toast.notSignedIn'));
      return;
    }
    const res = await fetch(`/api/sources/${encodeURIComponent(s.id)}/raw`, {
      headers: { Authorization: `Bearer ${token}` },
      credentials: 'same-origin',
    });
    if (!res.ok) {
      toast.error(t('sources.toast.downloadFailed', { status: res.status, statusText: res.statusText }));
      return;
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = s.filename;
    a.click();
    URL.revokeObjectURL(url);
  };

  // PDF/XLSX kinds are downloadable via /sources/<id>/raw (slice 15d).
  // Other kinds (text/url) have no on-disk binary worth downloading.
  const showDownload = s.kind === 'pdf' || s.kind === 'xlsx' || s.kind === 'docx' || s.kind === 'pdf_generated';
  // Re-OCR / Ingest only make sense for OCR-driven kinds (PDFs).
  // Generated artifacts (xlsx) skip the OCR pipeline entirely.
  const ocrEligible = s.kind === 'pdf';
  const showIngest = ocrEligible && (s.status === 'ocr_complete' || s.status === 'failed');
  const showReocr = ocrEligible && (s.status === 'stored' || s.status === 'failed');
  if (!showIngest && !showReocr && !showDownload) {
    return <span className="text-xs text-muted-foreground">{'—'}</span>;
  }
  return (
    <div className="inline-flex flex-wrap justify-end gap-1">
      {showDownload && (
        <button
          type="button"
          onClick={() => void handleDownload()}
          className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted"
          title={t('sources.action.downloadHint', { filename: s.filename })}
        >
          <Download size={14} />
          {t('sources.action.download')}
        </button>
      )}
      {showReocr && (
        <button
          type="button"
          disabled={busy}
          onClick={onReocr}
          className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
          title={t('sources.action.reocrHint')}
        >
          <RefreshCcw size={14} />
          {t('sources.action.reocr')}
        </button>
      )}
      {showIngest && (
        <button
          type="button"
          disabled={busy}
          onClick={onIngest}
          className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
          title={t('sources.action.ingestHint')}
        >
          <Play size={14} />
          {t('sources.action.ingest')}
        </button>
      )}
    </div>
  );
}

// downloadSource is now inlined inside SourceActions so it has access to t().
// Kept as a standalone comment for documentation purposes — the function
// fetches the binary via /api/sources/<id>/raw with the bearer token header
// and triggers a browser download. We can't use a plain <a href> because the
// endpoint is auth-gated and Authorization headers don't tag along on link
// clicks. Toast surfaces failures so 401s and 404s aren't silent.

function formatUploadSummary(filename: string, res: UploadResponse, t: ReturnType<typeof useLocale>['t']): string {
  if (res.duplicate) return t('sources.toast.uploadDuplicate', { filename, id: res.id });
  const parts = [filename, res.id];
  if (res.page_count) parts.push(t('sources.pageCount', { count: res.page_count }));
  if (res.note) parts.push(res.note);
  return parts.join(' · ');
}

function SourceInboxSkeleton() {
  return (
    <div className="p-6 space-y-6">
      <Skeleton className="h-8 w-32" />
      <Skeleton className="h-32 w-full" />
      {[0, 1].map((i) => (
        <div key={i} className="space-y-2">
          <Skeleton className="h-4 w-40" />
          <div className="rounded-lg border overflow-x-auto">
            {[0, 1].map((j) => (
              <div key={j} className="border-t first:border-t-0 px-3 py-3 flex items-center gap-3">
                <Skeleton className="h-4 flex-1" />
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-3 w-12" />
                <Skeleton className="h-7 w-20" />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
