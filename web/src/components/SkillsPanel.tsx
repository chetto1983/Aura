import { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Sparkles,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  Trash2,
  Download,
  Search,
  Lock,
  Loader2,
  Store,
} from 'lucide-react';
import { toast } from 'sonner';
import { api, ApiError } from '@/api';
import { useApi } from '@/hooks/useApi';
import { Skeleton } from '@/components/ui/skeleton';
import type { SkillDetail, SkillSummary, SkillCatalogItem } from '@/types/api';

type Tab = 'local' | 'catalog';

export function SkillsPanel() {
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = (searchParams.get('tab') as Tab) || 'local';
  const setTab = useCallback((t: Tab) => {
    setSearchParams((prev) => { prev.set('tab', t); return prev; }, { replace: true });
  }, [setSearchParams]);
  // adminUnknown stays true until we attempt a write — at that point, a 403
  // tells us SKILLS_ADMIN is off and we surface the instructions banner.
  const [adminGated, setAdminGated] = useState(false);

  return (
    <div className="p-6 space-y-4">
      <header className="space-y-2">
        <div>
          <h1 className="text-2xl font-semibold">Skills</h1>
          <p className="text-xs text-muted-foreground mt-1">
            Local SKILL.md playbooks the LLM sees on every turn — plus the public skills.sh catalog for one-click installs.
          </p>
        </div>
        <div className="flex gap-1 border-b">
          <TabButton active={tab === 'local'} onClick={() => setTab('local')} icon={<Sparkles size={14} />}>
            Local
          </TabButton>
          <TabButton active={tab === 'catalog'} onClick={() => setTab('catalog')} icon={<Store size={14} />}>
            Catalog
          </TabButton>
        </div>
      </header>

      {adminGated && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300 flex items-start gap-2">
          <Lock size={14} className="mt-0.5 shrink-0" />
          <div>
            <p className="font-medium">Admin actions disabled</p>
            <p className="opacity-90 mt-0.5">
              Set <code className="font-mono">SKILLS_ADMIN=true</code> in <code className="font-mono">.env</code> and restart Aura to enable installing and deleting skills from the dashboard.
            </p>
          </div>
        </div>
      )}

      {tab === 'local' ? (
        <LocalSkillsView onAdminBlocked={() => setAdminGated(true)} />
      ) : (
        <CatalogView onAdminBlocked={() => setAdminGated(true)} />
      )}
    </div>
  );
}

function TabButton({
  active,
  onClick,
  icon,
  children,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`-mb-px inline-flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm transition-colors ${
        active
          ? 'border-primary text-primary font-medium'
          : 'border-transparent text-muted-foreground hover:text-foreground'
      }`}
    >
      {icon}
      {children}
    </button>
  );
}

function LocalSkillsView({ onAdminBlocked }: { onAdminBlocked: () => void }) {
  const fetcher = useCallback(() => api.skills(), []);
  const { data, error, loading, refetch } = useApi(fetcher);
  const [open, setOpen] = useState<Record<string, SkillDetail | 'loading' | 'error' | undefined>>({});
  const [deletingNames, setDeletingNames] = useState<Set<string>>(new Set());

  const toggle = useCallback(async (name: string) => {
    const wasOpen = open[name] !== undefined;
    setOpen((prev) => {
      if (prev[name] !== undefined) {
        const next = { ...prev };
        delete next[name];
        return next;
      }
      return { ...prev, [name]: 'loading' };
    });
    if (wasOpen) return;
    try {
      const detail = await api.skill(name);
      setOpen((prev) => ({ ...prev, [name]: detail }));
    } catch (err) {
      setOpen((prev) => ({ ...prev, [name]: 'error' }));
      console.error('skill load failed', err);
    }
  }, [open]);

  const handleDelete = useCallback(async (name: string) => {
    if (!window.confirm(`Delete skill "${name}"? This removes the local SKILL.md file.`)) return;
    setDeletingNames((prev) => new Set(prev).add(name));
    const id = toast.loading(`Deleting ${name}…`);
    try {
      await api.deleteSkill(name);
      toast.success(`Deleted ${name}`, { id });
      refetch();
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        onAdminBlocked();
        toast.error('Delete blocked: SKILLS_ADMIN=false', { id });
      } else {
        const msg = err instanceof Error ? err.message : String(err);
        toast.error(`Delete failed: ${msg}`, { id });
      }
    } finally {
      setDeletingNames((prev) => {
        const next = new Set(prev);
        next.delete(name);
        return next;
      });
    }
  }, [refetch, onAdminBlocked]);

  if (loading && !data) return <LocalSkeleton />;
  if (error && !data) return <div className="text-sm text-destructive">Error: {error.message}</div>;
  if (!data) return null;

  return (
    <div className="space-y-3">
      <div className="flex justify-between items-center">
        <span className="text-xs text-muted-foreground">{data.length} loaded</span>
      </div>
      {data.length === 0 ? (
        <EmptyLocal />
      ) : (
        <div className="rounded-lg border overflow-hidden">
          {data.map((s) => (
            <LocalSkillRow
              key={s.name}
              skill={s}
              detail={open[s.name]}
              onToggle={() => void toggle(s.name)}
              onDelete={() => void handleDelete(s.name)}
              deleting={deletingNames.has(s.name)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function LocalSkillRow({
  skill,
  detail,
  onToggle,
  onDelete,
  deleting,
}: {
  skill: SkillSummary;
  detail: SkillDetail | 'loading' | 'error' | undefined;
  onToggle: () => void;
  onDelete: () => void;
  deleting: boolean;
}) {
  const isOpen = detail !== undefined;
  return (
    <div className="border-b last:border-b-0">
      <div className="flex items-start gap-2 px-2 hover:bg-muted/30">
        <button
          type="button"
          onClick={onToggle}
          className="flex flex-1 items-start gap-3 py-3 pl-2 text-left min-w-0"
        >
          {isOpen ? <ChevronDown size={16} className="mt-0.5 shrink-0" /> : <ChevronRight size={16} className="mt-0.5 shrink-0" />}
          <div className="flex-1 min-w-0">
            <span className="font-mono text-sm font-medium">{skill.name}</span>
            {skill.description && (
              <p className="mt-1 text-xs text-muted-foreground line-clamp-2">{skill.description}</p>
            )}
          </div>
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={deleting}
          title="Delete this skill"
          className="my-2 mr-2 inline-flex items-center gap-1 rounded-md border border-destructive/30 px-2 py-1 text-xs text-destructive hover:bg-destructive/10 disabled:opacity-50"
        >
          {deleting ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
          Delete
        </button>
      </div>
      {isOpen && (
        <div className="bg-muted/20 border-t px-12 py-3">
          {detail === 'loading' && <Skeleton className="h-32 w-full" />}
          {detail === 'error' && (
            <div className="flex items-center gap-2 text-sm text-destructive">
              <AlertTriangle size={14} />
              Failed to load SKILL.md
            </div>
          )}
          {typeof detail === 'object' && detail && (
            <>
              {detail.truncated && (
                <div className="mb-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-700 dark:text-amber-400">
                  Content truncated for the dashboard. Open the file on disk for the full body.
                </div>
              )}
              <pre className="text-xs whitespace-pre-wrap font-mono leading-relaxed text-muted-foreground">
                {detail.content}
              </pre>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function CatalogView({ onAdminBlocked }: { onAdminBlocked: () => void }) {
  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const fetcher = useCallback(() => api.skillsCatalog(debounced || undefined), [debounced]);
  const { data, error, loading } = useApi(fetcher);
  const [installing, setInstalling] = useState<string | null>(null);

  // Debounce the query input so we don't hammer skills.sh on every keystroke.
  useDebounce(query, 350, setDebounced);

  const handleInstall = useCallback(async (item: SkillCatalogItem) => {
    const key = `${item.source}::${item.skill_id ?? ''}`;
    setInstalling(key);
    const id = toast.loading(`Installing ${item.name}…`, {
      description: 'npx skills add ' + item.source + (item.skill_id ? ` --skill ${item.skill_id}` : ''),
    });
    try {
      const resp = await api.installSkill({ source: item.source, skill_id: item.skill_id });
      if (resp.ok) {
        toast.success(`Installed ${item.name}`, { id, description: 'Restart not required — the loader picks up new skills on the next chat turn.' });
      } else {
        toast.error(`Install failed`, {
          id,
          description: resp.error ?? 'See dashboard logs for details.',
          duration: 8000,
        });
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        onAdminBlocked();
        toast.error('Install blocked: SKILLS_ADMIN=false', { id });
      } else {
        const msg = err instanceof Error ? err.message : String(err);
        toast.error(`Install failed: ${msg}`, { id });
      }
    } finally {
      setInstalling(null);
    }
  }, [onAdminBlocked]);

  return (
    <div className="space-y-3">
      <div className="relative">
        <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
        <input
          type="text"
          placeholder="Search skills.sh…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="w-full rounded-md border bg-background pl-8 pr-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-primary/30"
        />
      </div>

      {loading && !data && <CatalogSkeleton />}
      {error && !data && (
        <div className="text-sm text-destructive">Catalog unavailable: {error.message}</div>
      )}
      {data && data.length === 0 && (
        <div className="rounded-lg border border-dashed py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {debounced ? `No catalog matches for "${debounced}".` : 'Catalog returned no entries.'}
          </p>
        </div>
      )}
      {data && data.length > 0 && (
        <div className="rounded-lg border overflow-hidden">
          {data.map((item) => {
            const key = `${item.source}::${item.skill_id ?? ''}`;
            return (
              <div key={key} className="flex items-start gap-3 border-b last:border-b-0 px-4 py-3 hover:bg-muted/20">
                <div className="flex-1 min-w-0">
                  <div className="flex items-baseline gap-2 flex-wrap">
                    <span className="font-medium text-sm">{item.name}</span>
                    {item.skill_id && item.skill_id !== item.name && (
                      <span className="font-mono text-[10px] text-muted-foreground">{item.skill_id}</span>
                    )}
                  </div>
                  <div className="mt-1 flex items-center gap-3 text-xs text-muted-foreground">
                    <span className="font-mono truncate">{item.source}</span>
                    <span className="shrink-0">↓ {item.installs.toLocaleString()}</span>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => void handleInstall(item)}
                  disabled={installing === key}
                  className="inline-flex items-center gap-1 rounded-md border border-primary/30 bg-primary/5 px-3 py-1.5 text-xs text-primary hover:bg-primary/10 disabled:opacity-50 shrink-0"
                >
                  {installing === key ? <Loader2 size={12} className="animate-spin" /> : <Download size={12} />}
                  Install
                </button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// useDebounce calls setOut only after `value` has been stable for `delay` ms.
function useDebounce<T>(value: T, delay: number, setOut: (v: T) => void) {
  useEffect(() => {
    const id = setTimeout(() => setOut(value), delay);
    return () => clearTimeout(id);
  }, [value, delay, setOut]);
}

function EmptyLocal() {
  return (
    <div className="rounded-lg border border-dashed py-12 text-center">
      <div className="flex flex-col items-center gap-2 text-muted-foreground">
        <Sparkles size={32} className="opacity-40" />
        <p className="text-sm font-medium">No local skills</p>
        <p className="text-xs max-w-md mx-auto">
          Browse the <span className="text-foreground">Catalog</span> tab to install one from skills.sh, or drop a folder under <code className="font-mono">skills/&lt;name&gt;/SKILL.md</code> with <code className="font-mono">name</code> + <code className="font-mono">description</code> frontmatter.
        </p>
      </div>
    </div>
  );
}

function LocalSkeleton() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-4 w-16" />
      <div className="rounded-lg border overflow-hidden">
        {[0, 1, 2].map((i) => (
          <div key={i} className="border-b last:border-b-0 px-4 py-3 flex items-start gap-3">
            <Skeleton className="h-4 w-4 mt-0.5" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-3 w-full max-w-md" />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function CatalogSkeleton() {
  return (
    <div className="rounded-lg border overflow-hidden">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="border-b last:border-b-0 px-4 py-3 flex items-start gap-3">
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3 w-64" />
          </div>
          <Skeleton className="h-7 w-20" />
        </div>
      ))}
    </div>
  );
}
