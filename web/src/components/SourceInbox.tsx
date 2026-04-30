import { useCallback, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Upload } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
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
  const fileInputRef = useRef<HTMLInputElement>(null);

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

  if (loading && !data) return <div className="p-6 text-sm text-muted-foreground">Loading…</div>;
  if (error && !data) return <ErrorCard error={error} />;
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
            <div className="rounded-lg border overflow-hidden">
              <table className="w-full text-sm">
                <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                  <tr>
                    <th className="text-left py-2 px-3 font-medium">Filename</th>
                    <th className="text-left py-2 px-3 font-medium">Created</th>
                    <th className="text-right py-2 px-3 font-medium">Pages</th>
                    <th className="text-left py-2 px-3 font-medium">Wiki</th>
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
function ErrorCard({ error }: { error: Error }) {
  return <div className="p-6 text-sm text-destructive">Error: {error.message}</div>;
}
function shortDate(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  const d = new Date(iso);
  return d.toLocaleString();
}
