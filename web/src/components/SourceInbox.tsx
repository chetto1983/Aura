import { useCallback, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Upload, Play, RefreshCcw } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { ErrorCard } from '@/components/common/ErrorCard';
import { Skeleton } from '@/components/ui/skeleton';
import type { SourceSummary, UploadResponse } from '@/types/api';

const POLL_MS = 5000;
const STATUS_ORDER: SourceSummary['status'][] = ['failed', 'stored', 'ocr_complete', 'ingested'];
const STATUS_LABEL: Record<SourceSummary['status'], string> = {
  failed: '❌ Failed',
  stored: '📄 Stored (awaiting OCR)',
  ocr_complete: '✅ OCR complete (awaiting ingest)',
  ingested: '📚 Ingested',
};

export function SourceInbox() {
  const fetcher = useCallback(() => api.sources(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher, POLL_MS);
  const [uploading, setUploading] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  // Per-row in-flight tracking so the same button can't be double-clicked.
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());
  const fileInputRef = useRef<HTMLInputElement>(null);

  const setBusy = useCallback((id: string, on: boolean) => {
    setBusyIds((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);

  const handleIngest = useCallback(async (s: SourceSummary) => {
    setBusy(s.id, true);
    const toastId = toast.loading(`Ingesting ${s.filename}…`);
    try {
      const res = await api.ingestSource(s.id);
      toast.success(`${s.filename} · ${res.note ?? 'ingested'}`, { id: toastId });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Ingest failed: ${s.filename}\n${msg}`, { id: toastId });
    } finally {
      setBusy(s.id, false);
    }
  }, [refetch, setBusy]);

  const handleReocr = useCallback(async (s: SourceSummary) => {
    setBusy(s.id, true);
    const toastId = toast.loading(`Re-OCRing ${s.filename}…`);
    try {
      const res = await api.reocrSource(s.id);
      toast.success(`${s.filename} · ${res.note ?? 'OCR redone'}`, { id: toastId });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Re-OCR failed: ${s.filename}\n${msg}`, { id: toastId });
    } finally {
      setBusy(s.id, false);
    }
  }, [refetch, setBusy]);

  const handleFiles = useCallback(async (files: FileList | File[] | null) => {
    if (!files || files.length === 0) return;
    const list = Array.from(files);
    const pdfs = list.filter((f) => f.name.toLowerCase().endsWith('.pdf'));
    const skipped = list.length - pdfs.length;
    if (skipped > 0) {
      toast.warning(`Skipped ${skipped} non-PDF file${skipped === 1 ? '' : 's'}`);
    }
    if (pdfs.length === 0) return;

    setUploading(true);
    try {
      for (const f of pdfs) {
        const id = toast.loading(`Uploading ${f.name}…`);
        try {
          const res: UploadResponse = await api.uploadSource(f);
          toast.success(formatUploadSummary(f.name, res), { id });
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err);
          toast.error(`Upload failed: ${f.name}\n${msg}`, { id });
        }
        // Poll-friendly: each upload triggers a refresh so the table updates
        // without waiting for the 5s tick.
        refetch();
      }
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  }, [refetch]);

  if (loading && !data) return <SourceInboxSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Failed to load sources" onRetry={refetch} />;
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
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Sources</h1>
        {stale && <StalePill />}
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
          No sources yet — drop a PDF above, or upload one in Telegram.
        </p>
      )}

      {STATUS_ORDER.map((status) => {
        const rows = grouped[status];
        if (!rows || rows.length === 0) return null;
        return (
          <section key={status}>
            <h2 className="text-sm font-medium text-muted-foreground mb-2">
              {STATUS_LABEL[status]} <span className="ml-1 tabular-nums">({rows.length})</span>
            </h2>
            <div className="rounded-lg border overflow-x-auto">
              <table className="w-full text-sm min-w-[600px]">
                <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                  <tr>
                    <th className="text-left py-2 px-3 font-medium">Filename</th>
                    <th className="text-left py-2 px-3 font-medium">Created</th>
                    <th className="text-right py-2 px-3 font-medium">Pages</th>
                    <th className="text-left py-2 px-3 font-medium">Wiki</th>
                    <th className="text-right py-2 px-3 font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((s) => (
                    <tr key={s.id} className="border-t hover:bg-muted/30">
                      <td className="py-2 px-3 font-mono text-xs">{s.filename}</td>
                      <td className="py-2 px-3 text-muted-foreground">{shortDate(s.created_at)}</td>
                      <td className="py-2 px-3 text-right tabular-nums">{s.page_count ?? '—'}</td>
                      <td className="py-2 px-3">
                        {s.wiki_pages?.length
                          ? s.wiki_pages.map((p) => (
                              <code key={p} className="text-xs">[[{p}]]</code>
                            ))
                          : <span className="text-muted-foreground">—</span>}
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
        {uploading ? 'Uploading…' : active ? 'Drop your PDF to upload' : 'Drag PDFs here, or click to browse'}
      </p>
      <p className="mt-1 text-xs text-muted-foreground">
        Same pipeline as Telegram: stored → OCR → auto-ingested into the wiki.
      </p>
    </button>
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
  const showIngest = s.status === 'ocr_complete' || s.status === 'failed';
  const showReocr = s.status === 'stored' || s.status === 'failed';
  if (!showIngest && !showReocr) {
    return <span className="text-xs text-muted-foreground">—</span>;
  }
  return (
    <div className="inline-flex gap-1">
      {showReocr && (
        <button
          type="button"
          disabled={busy}
          onClick={onReocr}
          className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
          title="Re-run Mistral OCR on this PDF"
        >
          <RefreshCcw size={12} />
          Re-OCR
        </button>
      )}
      {showIngest && (
        <button
          type="button"
          disabled={busy}
          onClick={onIngest}
          className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
          title="Compile OCR markdown into a wiki summary page"
        >
          <Play size={12} />
          Ingest
        </button>
      )}
    </div>
  );
}

function formatUploadSummary(filename: string, res: UploadResponse): string {
  if (res.duplicate) return `Duplicate: ${filename} already stored as ${res.id}`;
  const parts = [filename, res.id];
  if (res.page_count) parts.push(`${res.page_count} page${res.page_count === 1 ? '' : 's'}`);
  if (res.note) parts.push(res.note);
  return parts.join(' · ');
}

function StalePill() {
  return <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">⚠ stale</span>;
}
function shortDate(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  const d = new Date(iso);
  return d.toLocaleString();
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
