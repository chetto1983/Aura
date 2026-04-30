import { useCallback, useState } from 'react';
import { Sparkles, ChevronDown, ChevronRight, AlertTriangle } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { Skeleton } from '@/components/ui/skeleton';
import type { SkillDetail } from '@/types/api';

// SkillsPanel renders local SKILL.md packages loaded from SKILLS_PATH.
// Read-only — installation and editing live behind a future admin flow.
export function SkillsPanel() {
  const fetcher = useCallback(() => api.skills(), []);
  const { data, error, loading } = useApi(fetcher);
  const [open, setOpen] = useState<Record<string, SkillDetail | 'loading' | 'error' | undefined>>({});

  const toggle = useCallback(async (name: string) => {
    setOpen((prev) => {
      if (prev[name]) {
        const next = { ...prev };
        delete next[name];
        return next;
      }
      return { ...prev, [name]: 'loading' };
    });
    if (open[name]) return; // already open — collapse handled above
    try {
      const detail = await api.skill(name);
      setOpen((prev) => ({ ...prev, [name]: detail }));
    } catch (err) {
      setOpen((prev) => ({ ...prev, [name]: 'error' }));
      console.error('skill load failed', err);
    }
  }, [open]);

  if (loading && !data) return <SkillsSkeleton />;
  if (error && !data) {
    return <div className="p-6 text-sm text-destructive">Error: {error.message}</div>;
  }
  if (!data) return null;

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Skills</h1>
          <p className="text-xs text-muted-foreground mt-1">
            Local SKILL.md packages loaded from <code className="font-mono">SKILLS_PATH</code>. Each turn the LLM sees their description and applies the matching one.
          </p>
        </div>
        <span className="text-xs text-muted-foreground">{data.length} loaded</span>
      </header>

      {data.length === 0 ? (
        <div className="rounded-lg border border-dashed py-12 text-center">
          <div className="flex flex-col items-center gap-2 text-muted-foreground">
            <Sparkles size={32} className="opacity-40" />
            <p className="text-sm font-medium">No local skills</p>
            <p className="text-xs">
              Drop a folder under <code className="font-mono">skills/&lt;name&gt;/SKILL.md</code> with frontmatter (<code className="font-mono">name</code>, <code className="font-mono">description</code>) to teach Aura a new playbook.
            </p>
          </div>
        </div>
      ) : (
        <div className="rounded-lg border overflow-hidden">
          {data.map((s) => {
            const isOpen = open[s.name] !== undefined;
            const detail = open[s.name];
            return (
              <div key={s.name} className="border-b last:border-b-0">
                <button
                  type="button"
                  onClick={() => void toggle(s.name)}
                  className="w-full flex items-start gap-3 px-4 py-3 text-left hover:bg-muted/30"
                >
                  {isOpen ? <ChevronDown size={16} className="mt-0.5 shrink-0" /> : <ChevronRight size={16} className="mt-0.5 shrink-0" />}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2">
                      <span className="font-mono text-sm font-medium">{s.name}</span>
                    </div>
                    {s.description && (
                      <p className="mt-1 text-xs text-muted-foreground line-clamp-2">{s.description}</p>
                    )}
                  </div>
                </button>
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
          })}
        </div>
      )}
    </div>
  );
}

function SkillsSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-4 w-16" />
      </div>
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
