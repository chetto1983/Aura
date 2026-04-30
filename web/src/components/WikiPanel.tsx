import { useCallback, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';
import { RefreshCw } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';

export function WikiPanel() {
  const fetcher = useCallback(() => api.wikiPages(), []);
  const { data, error, loading, stale, refetch } = useApi(fetcher);
  const [filter, setFilter] = useState('');
  const [category, setCategory] = useState<string>('');
  const [rebuilding, setRebuilding] = useState(false);

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

  if (loading && !data) return <div className="p-6 text-sm text-muted-foreground">Loading…</div>;
  if (error && !data) return <div className="p-6 text-sm text-destructive">Error: {error.message}</div>;
  if (!data) return null;

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">Wiki</h1>
          {stale && <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">⚠ stale</span>}
        </div>
        <button
          type="button"
          disabled={rebuilding}
          onClick={() => void handleRebuild()}
          className="inline-flex items-center gap-1 rounded-md border px-3 py-1.5 text-sm hover:bg-muted disabled:opacity-50 disabled:cursor-wait"
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
          className="flex-1 min-w-[12rem] rounded-md border bg-background px-3 py-1.5 text-sm"
        />
        <button
          type="button"
          onClick={() => setCategory('')}
          className={`rounded-full px-2 py-0.5 text-xs ${category === '' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}
        >
          all
        </button>
        {categories.map((c) => (
          <button
            key={c}
            type="button"
            onClick={() => setCategory(c)}
            className={`rounded-full px-2 py-0.5 text-xs ${category === c ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}
          >
            {c}
          </button>
        ))}
      </div>

      <div className="rounded-lg border overflow-hidden">
        <table className="w-full text-sm">
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
              <tr><td colSpan={4} className="py-6 text-center text-sm text-muted-foreground">No matching pages</td></tr>
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
