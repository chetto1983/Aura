import { useCallback } from 'react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { SourceSummary } from '@/types/api';

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
  const { data, error, loading, stale } = useApi(fetcher, POLL_MS);

  if (loading && !data) return <div className="p-6 text-sm text-muted-foreground">Loading…</div>;
  if (error && !data) return <ErrorCard error={error} />;
  if (!data) return null;

  if (data.length === 0) {
    return (
      <div className="p-6">
        <h1 className="text-2xl font-semibold mb-4">Sources</h1>
        <p className="text-sm text-muted-foreground">
          No sources yet — upload a PDF in Telegram to get started.
        </p>
      </div>
    );
  }

  const grouped: Record<string, SourceSummary[]> = {};
  for (const s of data) (grouped[s.status] ??= []).push(s);

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Sources</h1>
        {stale && <StalePill />}
      </header>
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
