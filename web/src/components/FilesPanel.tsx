// FilesPanel - multi-root file manager dashboard panel.
//
// Wraps svar's React Filemanager widget so the operator can browse
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
// SVAR owns navigation, selection, native previews, and operation chrome;
// Aura persists destructive operations through its REST API.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Filemanager, Willow, WillowDark } from '@svar-ui/react-filemanager';
import '@svar-ui/react-filemanager/all.css';
import { useTranslation } from 'react-i18next';
import { HardDrive } from 'lucide-react';
import { toast } from 'sonner';

import { api, ApiError } from '@/api';
import { useAppTheme } from '@/hooks/useAppTheme';
import { cn } from '@/lib/utils';

type FileRoot = 'wiki' | 'sources' | 'workspace' | 'skills';

const ROOTS: { key: FileRoot; labelKey: string }[] = [
  { key: 'wiki', labelKey: 'files.root.wiki' },
  { key: 'sources', labelKey: 'files.root.sources' },
  { key: 'workspace', labelKey: 'files.root.workspace' },
  { key: 'skills', labelKey: 'files.root.skills' },
];

const ROOT_ACCENTS: Record<FileRoot, string> = {
  wiki: 'bg-blue-500',
  sources: 'bg-amber-500',
  workspace: 'bg-emerald-500',
  skills: 'bg-violet-500',
};

interface SvarItem {
  id: string;
  type: 'folder' | 'file';
  size?: number;
  date?: Date;
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

function formatBytes(size?: number): string {
  if (!size) return '0 b';
  const units = ['b', 'kb', 'Mb', 'Gb'];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

export function FilesPanel() {
  const { t } = useTranslation();
  const { theme } = useAppTheme();
  const [root, setRoot] = useState<FileRoot>('wiki');
  const [items, setItems] = useState<SvarItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const requestSeq = useRef(0);

  const refresh = useCallback(async () => {
    const seq = ++requestSeq.current;
    setLoading(true);
    setError(null);
    try {
      const rows = await api.filesTree(root, '', true);
      if (seq !== requestSeq.current) return;
      setItems(toSvarItems(rows));
    } catch (err) {
      if (seq !== requestSeq.current) return;
      const msg = err instanceof ApiError ? err.message : (err as Error).message;
      setError(msg);
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, [root]);

  useEffect(() => {
    void Promise.resolve().then(refresh);
  }, [refresh]);

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
        await refresh();
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : (err as Error).message);
      }
    },
    [root, refresh, t],
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

  // svar's Filemanager dispatches operations through its internal event
  // bus. We hook it via `init(api)` so we can intercept by op name and
  // route to our REST calls. Earlier revisions of this panel passed a
  // single `onAction` prop, but svar has no such prop: its real surface
  // is the per-op event names (`select-file`, `open-file`, `delete-files`,
  // `rename-file`, `create-file`). Passing `onAction` looked typed-OK
  // (the index signature `[key: ${"on"}${string}]: ...` accepts anything
  // starting with "on") but the widget never invoked it, leaving every
  // click, delete, rename, and mkdir as a silent no-op.
  const initApi = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (api: any) => {
      api.on('delete-files', (ev: { ids?: string[] }) => {
        (ev?.ids ?? []).forEach((id) => void handleDelete(id));
      });
      api.on('rename-file', (ev: { id?: string; name?: string; newId?: string }) => {
        // svar pre-computes the new id; fall back to id+name if we got
        // an older payload shape.
        const from = ev?.id;
        if (!from) return;
        const to = ev?.newId
          ?? (() => {
            const parts = from.split('/');
            parts.pop();
            return [...parts, ev?.name ?? ''].join('/') || '/' + (ev?.name ?? '');
          })();
        void handleRename(from, to);
      });
      api.on('create-file', (ev: { file?: { name?: string; type?: string }; parent?: string; newId?: string }) => {
        // Only folder creation is supported by the backend's /mkdir route.
        if (ev?.file?.type !== 'folder') return;
        const target = ev?.newId
          ?? ((ev?.parent ?? '/') + (ev?.parent && ev.parent !== '/' ? '/' : '') + (ev?.file?.name ?? ''));
        if (target && target !== '/') void handleMkdir(target);
      });
    },
    [handleDelete, handleRename, handleMkdir],
  );

  const rootSize = useMemo(
    () => items.reduce((total, item) => total + (item.type === 'file' ? item.size ?? 0 : 0), 0),
    [items],
  );
  const ThemeShell = theme === 'light' ? Willow : WillowDark;

  return (
    <div className="files-workbench">
      <section className="files-window" aria-label={t('files.title')}>
        <header className="files-window-toolbar files-window-toolbar-compact">
          <div className="files-window-title">{t('files.title')}</div>
          <div className="files-root-switcher" aria-label={t('files.roots')}>
            {ROOTS.map(({ key, labelKey }) => (
              <button
                key={key}
                type="button"
                onClick={() => setRoot(key)}
                className={cn(root === key && 'is-active')}
              >
                <span className={cn('files-root-dot', ROOT_ACCENTS[key])} />
                {t(labelKey)}
              </button>
            ))}
          </div>
        </header>

        <div className="files-root-strip">
          <div className="files-root-help">
            <HardDrive size={15} aria-hidden="true" />
            <span>{t(`files.help.${root}`)}</span>
          </div>
          <div className="files-storage files-storage-inline">
            <div className="files-storage-bar">
              <span style={{ width: `${Math.min(100, Math.max(8, rootSize / 50000))}%` }} />
            </div>
            <p>{t('files.storageUsed', { size: formatBytes(rootSize) })}</p>
          </div>
        </div>

        <div className="files-window-body files-window-body-native">
          <main className="files-manager-pane">
            {error && (
              <div className="files-error" role="alert">{error}</div>
            )}
            <div className="files-manager-frame">
              {loading ? (
                <div className="files-loading">{t('files.loading')}</div>
              ) : (
                <ThemeShell>
                  <Filemanager data={items} init={initApi} mode="table" preview />
                </ThemeShell>
              )}
            </div>
          </main>
        </div>
      </section>
    </div>
  );
}

export default FilesPanel;
