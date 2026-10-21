import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { errorMessage, type DocumentsPage } from './documentsApi';
import { getJSON } from '@/api/json';
import { createFileManagerProvider } from '@/files/filesApi';

export interface UploadItem {
  readonly key: string;
  readonly name: string;
  readonly folder: string;
  readonly status: 'queued' | 'uploading' | 'waiting' | 'ready' | 'failed' | 'needs_file';
  readonly sourceKey?: string;
  readonly documentId?: string;
  readonly error?: string | undefined;
  readonly file?: File | undefined;
}
const storageKey = (identity: string) => `aura.documents.uploads.v1.${identity}`;
function restored(identity: string): UploadItem[] {
  if (!identity) return [];
  try {
    const raw: unknown = JSON.parse(sessionStorage.getItem(storageKey(identity)) ?? '[]');
    if (!Array.isArray(raw)) return [];
    return raw
      .filter((item: unknown): item is UploadItem => {
        if (!item || typeof item !== 'object') return false;
        const entry = item as Record<string, unknown>;
        return (
          typeof entry.key === 'string' &&
          typeof entry.name === 'string' &&
          typeof entry.folder === 'string'
        );
      })
      .slice(-100)
      .map((item) => ({
        ...item,
        status: item.sourceKey ? (item.documentId ? 'ready' : 'waiting') : 'needs_file',
      }));
  } catch {
    return [];
  }
}
function persist(identity: string, items: readonly UploadItem[]) {
  try {
    sessionStorage.setItem(
      storageKey(identity),
      JSON.stringify(items.map(({ file: _file, ...item }) => item)),
    );
  } catch {
    /* The in-memory queue remains usable when storage is unavailable. */
  }
}

export function useDocumentUploads(identity: string) {
  const client = useQueryClient();
  const [state, setState] = useState(() => ({ identity, items: restored(identity) }));
  if (state.identity !== identity) setState({ identity, items: restored(identity) });
  const lifetime = useRef<{
    identity: string;
    controller: AbortController;
    running: boolean;
  } | null>(null);
  const items = state.identity === identity ? state.items : [];
  const itemsRef = useRef(items);
  useEffect(() => {
    const session = { identity, controller: new AbortController(), running: false };
    lifetime.current = session;
    const initial = restored(identity);
    itemsRef.current = initial;
    return () => {
      session.controller.abort();
    };
  }, [identity]);

  function update(
    session: NonNullable<typeof lifetime.current>,
    change: (current: UploadItem[]) => UploadItem[],
  ) {
    if (session.controller.signal.aborted || lifetime.current !== session) return;
    const next = change(itemsRef.current);
    itemsRef.current = next;
    persist(session.identity, next);
    setState({ identity: session.identity, items: next });
  }

  async function drain(session: NonNullable<typeof lifetime.current>) {
    if (session.running) return;
    session.running = true;
    try {
      while (!session.controller.signal.aborted) {
        const item = itemsRef.current.find((entry) => entry.status === 'queued' && entry.file);
        if (!item?.file) break;
        const patch = (values: Partial<UploadItem>) => {
          update(session, (current) =>
            current.map((entry) => (entry.key === item.key ? { ...entry, ...values } : entry)),
          );
        };
        patch({ status: 'uploading' });
        try {
          const form = new FormData();
          form.append('file', item.file);
          const uploaded = await createFileManagerProvider().send<{ id: string }>(
            `upload?id=${encodeURIComponent(item.folder)}`,
            'POST',
            form,
            undefined,
            session.controller.signal,
          );
          if (typeof uploaded.id !== 'string' || !uploaded.id.startsWith('/'))
            throw new Error('Invalid upload response');
          patch({
            status: 'waiting',
            sourceKey: uploaded.id.slice(1),
            file: undefined,
            error: undefined,
          });
        } catch (error) {
          patch({ status: 'failed', error: errorMessage(error) });
        }
      }
    } finally {
      session.running = false;
    }
  }
  const pendingKeys = items.flatMap((item) =>
    item.status === 'waiting' && item.sourceKey ? [item.sourceKey] : [],
  );
  const lookup = useQuery({
    queryKey: ['documents-upload-lookup', identity, pendingKeys],
    enabled: !!identity && pendingKeys.length > 0,
    queryFn: ({ signal }) => {
      const query = new URLSearchParams({ file_type: 'all', limit: '100' });
      pendingKeys.forEach((key) => {
        query.append('source_key', key);
      });
      return getJSON<DocumentsPage>(`/api/documents?${query.toString()}`, signal);
    },
    refetchInterval: 3000,
    retry: false,
  });
  useEffect(() => {
    const session = lifetime.current;
    if (
      !session ||
      session.identity !== identity ||
      session.controller.signal.aborted ||
      !lookup.data
    )
      return;
    const found = new Map(lookup.data.rows.map((row) => [row.source_key, row.id]));
    const next = itemsRef.current.map((item) => {
      const documentId = item.sourceKey ? found.get(item.sourceKey) : undefined;
      if (item.status !== 'waiting' || !documentId) return item;
      return { ...item, status: 'ready' as const, documentId };
    });
    if (next.every((item, index) => item === itemsRef.current[index])) return;
    itemsRef.current = next;
    persist(identity, next);
    setState({ identity, items: next });
    void client.invalidateQueries({ queryKey: ['documents', identity] });
  }, [lookup.data, identity, client]);

  return {
    items,
    lookupFailed: lookup.isError,
    add: (files: readonly File[]) => {
      const session = lifetime.current;
      if (!identity || session?.identity !== identity) return;
      update(session, (current) => [
        ...current,
        ...files.slice(0, 100 - current.length).map((file) => {
          const key = crypto.randomUUID();
          return {
            key,
            name: file.name,
            folder: `/uploads/${key}/`,
            file,
            status: 'queued' as const,
          };
        }),
      ]);
      void drain(session);
    },
    retry(key: string, file?: File) {
      const session = lifetime.current;
      if (!session || session.identity !== identity) return;
      update(session, (current) =>
        current.map((item) =>
          item.key === key && !item.sourceKey && (file ?? item.file)
            ? { ...item, file: file ?? item.file, status: 'queued', error: undefined }
            : item,
        ),
      );
      void drain(session);
    },
    dismiss(key: string) {
      const session = lifetime.current;
      if (session?.identity === identity)
        update(session, (current) =>
          current.filter(
            (item) => item.key !== key || item.status === 'uploading' || item.status === 'queued',
          ),
        );
    },
  };
}
