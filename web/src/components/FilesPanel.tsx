// FilesPanel — multi-root file manager dashboard panel.
//
// Wraps svar's React Filemanager widget so the operator can browse and edit
// Aura's persistent state without shell access. Four roots:
//
//   wiki       LLM-curated markdown pages (writes reindex Qdrant)
//   sources    immutable ingested PDFs/text (delete via /sources/{id})
//   workspace  LLM-writable scratch space (pure CRUD)
//   skills     installed skill bundles (pure CRUD)
//
// The widget consumes a flat list of items keyed by full path ("/sub/file");
// we fetch each root recursively via /files/{root}/tree?recursive=true.
//
// Editor: a minimal textarea for utf-8 files — the dashboard's job here is
// "look + fix", not full Markdown authoring (that lives in WikiPanel for
// curated pages). Binary files come back as base64 and the editor is
// disabled.
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Filemanager } from '@svar-ui/react-filemanager';
import '@svar-ui/react-filemanager/all.css';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';

import { api, ApiError } from '@/api';

type FileRoot = 'wiki' | 'sources' | 'workspace' | 'skills';

const ROOTS: { key: FileRoot; labelKey: string }[] = [
  { key: 'wiki', labelKey: 'files.root.wiki' },
  { key: 'sources', labelKey: 'files.root.sources' },
  { key: 'workspace', labelKey: 'files.root.workspace' },
  { key: 'skills', labelKey: 'files.root.skills' },
];

interface SvarItem {
  id: string;
  type: 'folder' | 'file';
  size?: number;
  date?: Date;
}

interface SelectedFile {
  root: FileRoot;
  path: string;
  size: number;
  modified: string;
  encoding: 'utf-8' | 'base64';
  content: string;
}

// toSvarItems flattens our backend's tree into the {id, type, size, date}
// shape svar expects. Paths come back forward-slash-relative and we prefix
// "/" so svar treats them as absolute within the root.
function toSvarItems(rows: { name: string; type: 'file' | 'dir'; size?: number; modified?: string }[]): SvarItem[] {
  return rows.map((row) => ({
    id: '/' + row.name,
    type: row.type === 'dir' ? 'folder' : 'file',
    size: row.size,
    date: row.modified ? new Date(row.modified) : undefined,
  }));
}

// stripLeadingSlash turns svar's "/foo/bar" id back into "foo/bar" for the
// backend query string. The backend rejects leading slashes as absolute
// paths.
function stripLeadingSlash(id: string): string {
  return id.startsWith('/') ? id.slice(1) : id;
}

export function FilesPanel() {
  const { t } = useTranslation();
  const [root, setRoot] = useState<FileRoot>('wiki');
  const [items, setItems] = useState<SvarItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<SelectedFile | null>(null);
  const [draft, setDraft] = useState<string>('');
  const [saving, setSaving] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const rows = await api.filesTree(root, '', true);
      setItems(toSvarItems(rows));
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : (err as Error).message;
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [root]);

  useEffect(() => {
    setSelected(null);
    setDraft('');
    refresh();
  }, [refresh]);

  const handleSelect = useCallback(
    async (id: string) => {
      const path = stripLeadingSlash(id);
      try {
        const file = await api.filesRead(root, path);
        const snapshot: SelectedFile = { ...file, root };
        setSelected(snapshot);
        setDraft(file.encoding === 'utf-8' ? file.content : '');
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : (err as Error).message);
      }
    },
    [root],
  );

  const handleSave = useCallback(async () => {
    if (!selected || selected.encoding !== 'utf-8') return;
    setSaving(true);
    try {
      await api.filesWrite(selected.root, selected.path, draft, 'utf-8');
      toast.success(t('files.save.ok', { path: selected.path }));
      setSelected({ ...selected, content: draft });
      await refresh();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : (err as Error).message);
    } finally {
      setSaving(false);
    }
  }, [selected, draft, refresh, t]);

  const handleDelete = useCallback(
    async (id: string) => {
      const path = stripLeadingSlash(id);
      // Sources need the dedicated endpoint so the memoryindex stays
      // consistent — the multi-root delete blocks recursive removal.
      if (root === 'sources') {
        // svar gives us the folder/file id; for sources, top-level dirs
        // are src_xxx. If the operator nuked one of those, route to the
        // memoryindex-aware endpoint.
        const segments = path.split('/');
        if (segments.length === 1 && segments[0].startsWith('src_')) {
          try {
            const resp = await api.deleteSource(segments[0]);
            toast.success(
              resp.memory_purged
                ? t('files.delete.sourceFull', { id: segments[0] })
                : t('files.delete.sourceWarning', { id: segments[0], warning: resp.memory_purge_warning ?? '' }),
            );
            await refresh();
            return;
          } catch (err) {
            toast.error(err instanceof ApiError ? err.message : (err as Error).message);
            return;
          }
        }
      }
      try {
        await api.filesDelete(root, path);
        toast.success(t('files.delete.ok', { path }));
        if (selected?.path === path) setSelected(null);
        await refresh();
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : (err as Error).message);
      }
    },
    [root, refresh, selected, t],
  );

  const handleRename = useCallback(
    async (from: string, to: string) => {
      try {
        await api.filesRename(root, stripLeadingSlash(from), stripLeadingSlash(to));
        toast.success(t('files.rename.ok', { from, to }));
        await refresh();
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : (err as Error).message);
      }
    },
    [root, refresh, t],
  );

  const handleMkdir = useCallback(
    async (path: string) => {
      try {
        await api.filesMkdir(root, stripLeadingSlash(path));
        toast.success(t('files.mkdir.ok', { path }));
        await refresh();
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : (err as Error).message);
      }
    },
    [root, refresh, t],
  );

  // svar's Filemanager emits operations via the `onAction` callback (one
  // function per op name). We translate them into our REST calls. The
  // widget owns presentation; we own persistence.
  const onAction = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (op: string, payload: any) => {
      switch (op) {
        case 'open-file':
        case 'select-file':
          if (payload?.id) void handleSelect(payload.id);
          break;
        case 'delete-files': {
          const ids: string[] = Array.isArray(payload?.ids) ? payload.ids : [];
          ids.forEach((id) => void handleDelete(id));
          break;
        }
        case 'rename-file':
          if (payload?.id && payload?.name) {
            const parts = (payload.id as string).split('/');
            parts.pop();
            const target = [...parts, payload.name].join('/') || '/' + payload.name;
            void handleRename(payload.id, target);
            break;
          }
          break;
        case 'create-folder':
          if (payload?.id) void handleMkdir(payload.id);
          break;
        default:
          // Unknown op — let the widget swallow it.
          break;
      }
    },
    [handleSelect, handleDelete, handleRename, handleMkdir],
  );

  const editor = useMemo(() => {
    if (!selected) {
      return (
        <div className="text-sm text-muted-foreground">
          {t('files.editor.empty')}
        </div>
      );
    }
    if (selected.encoding !== 'utf-8') {
      return (
        <div className="text-sm text-muted-foreground">
          {t('files.editor.binary', { size: selected.size })}
        </div>
      );
    }
    return (
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>{selected.path}</span>
          <span>{t('files.editor.size', { size: selected.size })}</span>
        </div>
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          spellCheck={false}
          aria-label={t('files.editor.size', { size: selected.size })}
          title={selected.path}
          placeholder={selected.path}
          className="min-h-[400px] w-full rounded border border-border bg-surface p-2 font-mono text-sm"
        />
        <div className="flex gap-2">
          <button
            type="button"
            onClick={handleSave}
            disabled={saving || draft === selected.content}
            className="rounded border border-border bg-brand px-3 py-1 text-sm text-white disabled:opacity-50"
          >
            {saving ? t('files.editor.saving') : t('files.editor.save')}
          </button>
          <button
            type="button"
            onClick={() => setDraft(selected.content)}
            disabled={draft === selected.content}
            className="rounded border border-border px-3 py-1 text-sm disabled:opacity-50"
          >
            {t('files.editor.revert')}
          </button>
        </div>
      </div>
    );
  }, [selected, draft, saving, handleSave, t]);

  return (
    <div className="flex h-full flex-col gap-3 p-4">
      <header className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">{t('files.title')}</h1>
        <div className="flex gap-1">
          {ROOTS.map(({ key, labelKey }) => (
            <button
              key={key}
              type="button"
              onClick={() => setRoot(key)}
              className={`rounded px-3 py-1 text-sm ${
                root === key ? 'bg-brand text-white' : 'border border-border'
              }`}
            >
              {t(labelKey)}
            </button>
          ))}
        </div>
      </header>
      <p className="text-xs text-muted-foreground">{t(`files.help.${root}`)}</p>
      {error && (
        <div className="rounded border border-error bg-error/10 p-2 text-sm">{error}</div>
      )}
      <div className="grid flex-1 grid-cols-1 gap-3 overflow-hidden lg:grid-cols-[2fr_3fr]">
        <div className="min-h-0 overflow-hidden rounded border border-border">
          {loading ? (
            <div className="p-4 text-sm text-muted-foreground">{t('files.loading')}</div>
          ) : (
            <Filemanager data={items} onAction={onAction} />
          )}
        </div>
        <div className="min-h-0 overflow-auto rounded border border-border p-3">{editor}</div>
      </div>
    </div>
  );
}

export default FilesPanel;
