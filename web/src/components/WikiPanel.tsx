import { useCallback, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { RefreshCw, BookText } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';

export function WikiPanel() {
  const fetcher = useCallback(() => api.wikiPages(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher);
  const [searchParams, setSearchParams] = useSearchParams();
  const filter = searchParams.get('q') ?? '';
  const category = searchParams.get('cat') ?? '';
  const [rebuilding, setRebuilding] = useState(false);

  const setFilter = useCallback((q: string) => {
    setSearchParams((prev) => { if (q) prev.set('q', q); else prev.delete('q'); return prev; }, { replace: true });
  }, [setSearchParams]);

  const setCategory = useCallback((cat: string) => {
    setSearchParams((prev) => { if (cat) prev.set('cat', cat); else prev.delete('cat'); return prev; }, { replace: true });
  }, [setSearchParams]);

  const handleRebuild = useCallback(async () => {
    setRebuilding(true);
    const id = toast.loading('Rebuilding wiki index…');
    try {
      await api.rebuildWikiIndex();
      toast.success('Wiki index rebuilt', { id });
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Rebuild failed: ${msg}`, { id });
    } finally {
      setRebuilding(false);
    }
  }, [refetch]);

  const categories = useMemo(() => {
    if (!data) return [] as string[];
    return Array.from(new Set(data.map((p) => p.category).filter((c): c is string => !!c))).sort();
  }, [data]);

  const filtered = useMemo(() => {
    if (!data) return [];
    const f = filter.toLowerCase();
    return data.filter(
      (p) =>
        (!category || p.category === category) &&
        (f === '' || p.title.toLowerCase().includes(f) || p.slug.includes(f))
    );
  }, [data, filter, category]);

  if (loading && !data) return <WikiSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Failed to load wiki" onRetry={refetch} />;
  if (!data) return null;

  return (
    <div className="p-6 space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">Wiki</h1>
          {stale && <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">⚠ stale</span>}
        </div>
        <button
          type="button"
          disabled={rebuilding}
          onClick={() => void handleRebuild()}
          className="inline-flex min-h-11 items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
          title="Regenerate wiki/index.md from current pages"
        >
          <RefreshCw size={14} />
          Rebuild index
        </button>
      </header>

      <div className="flex flex-wrap items-center gap-2">
        <input
          type="text"
          placeholder="Search title or slug…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="min-h-11 flex-1 min-w-[12rem] rounded-md border bg-background px-3 py-2 text-sm"
        />
        <button
          type="button"
          onClick={() => setCategory('')}
          className={`inline-flex min-h-11 items-center rounded-full px-4 py-2 text-sm ${category === '' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}
        >
          all
        </button>
        {categories.map((c) => (
          <button
            key={c}
            type="button"
            onClick={() => setCategory(c)}
            className={`inline-flex min-h-11 items-center rounded-full px-4 py-2 text-sm ${category === c ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}
          >
            {c}
          </button>
        ))}
      </div>

      <div className="space-y-2 md:hidden">
        {filtered.map((p) => (
          <article key={p.slug} className="rounded-lg border bg-card p-3">
            <Link
              to={`/wiki/${p.slug}`}
              className="flex min-h-11 items-center text-sm font-medium text-primary underline-offset-2 hover:underline"
            >
              {p.title}
            </Link>
            <div className="mt-2 grid gap-1 text-xs text-muted-foreground">
              <p>{p.category ?? 'No category'}</p>
              <p>{p.tags?.length ? p.tags.map((t) => `#${t}`).join(' ') : 'No tags'}</p>
              <p>Updated {shortDate(p.updated_at)}</p>
            </div>
          </article>
        ))}
        {filtered.length === 0 && (
          <div className="rounded-lg border px-4 py-10 text-center">
            {data.length === 0 ? (
              <div className="flex flex-col items-center gap-2 text-muted-foreground">
                <BookText size={32} className="opacity-40" />
                <p className="text-sm font-medium">No wiki pages yet</p>
                <p className="text-xs">Drop a PDF on /sources or chat with the bot - pages appear after the first ingest.</p>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No pages match your filter</p>
            )}
          </div>
        )}
      </div>

      <div className="hidden rounded-lg border overflow-x-auto md:block">
        <table className="w-full text-sm min-w-[500px]">
          <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
            <tr>
              <th className="text-left py-2 px-3 font-medium">Title</th>
              <th className="text-left py-2 px-3 font-medium">Category</th>
              <th className="text-left py-2 px-3 font-medium">Tags</th>
              <th className="text-left py-2 px-3 font-medium">Updated</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((p) => (
              <tr key={p.slug} className="border-t hover:bg-muted/30">
                <td className="py-2 px-3">
                  <Link to={`/wiki/${p.slug}`} className="text-primary underline-offset-2 hover:underline">
                    {p.title}
                  </Link>
                </td>
                <td className="py-2 px-3 text-muted-foreground">{p.category ?? '—'}</td>
                <td className="py-2 px-3 text-xs text-muted-foreground">
                  {p.tags?.length ? p.tags.map((t) => `#${t}`).join(' ') : '—'}
                </td>
                <td className="py-2 px-3 text-xs text-muted-foreground">{shortDate(p.updated_at)}</td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={4} className="py-12 text-center">
                  {data.length === 0 ? (
                    <div className="flex flex-col items-center gap-2 text-muted-foreground">
                      <BookText size={32} className="opacity-40" />
                      <p className="text-sm font-medium">No wiki pages yet</p>
                      <p className="text-xs">Drop a PDF on /sources or chat with the bot — pages appear after the first ingest.</p>
                    </div>
                  ) : (
                    <p className="text-sm text-muted-foreground">No pages match your filter</p>
                  )}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function shortDate(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  return new Date(iso).toLocaleDateString();
}

function WikiSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-24" />
        <Skeleton className="h-9 w-32" />
      </div>
      <Skeleton className="h-9 w-full" />
      <div className="rounded-lg border overflow-hidden">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="border-t first:border-t-0 px-3 py-3 flex items-center gap-3">
            <Skeleton className="h-4 flex-1" />
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-3 w-12" />
          </div>
        ))}
      </div>
    </div>
  );
}
