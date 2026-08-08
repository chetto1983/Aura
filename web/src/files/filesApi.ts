// The file manager speaks its own REST dialect, so these three calls are the whole client.
// The mount mirrors the component's official Go reference backend
// (svar-widgets/filemanager-backend-go): a root listing, a folder listing keyed by the
// percent-encoded folder id, and a direct byte stream.
const base = '/api/filemanager';

export interface FileEntry {
  readonly id: string;
  readonly type: 'file' | 'folder';
  readonly size?: number;
  readonly date?: Date;
  readonly lazy?: boolean;
}

type RawFileEntry = Omit<FileEntry, 'date'> & { readonly date?: string };

/** Lists one folder. The empty id is the root. */
export async function loadFolder(id = ''): Promise<FileEntry[]> {
  const suffix = id === '' || id === '/' ? '' : `/${encodeURIComponent(id)}`;
  const res = await fetch(`${base}/files${suffix}`, { credentials: 'same-origin' });
  if (!res.ok) throw new Error(`HTTP ${String(res.status)}`);
  return ((await res.json()) as readonly RawFileEntry[]).map(withParsedDate);
}

/**
 * The widget sorts and formats on a Date, so the wire string has to become one -- the same
 * step the component's own backend demo performs before handing data over.
 */
function withParsedDate(entry: RawFileEntry): FileEntry {
  const { date, ...rest } = entry;
  return date === undefined ? rest : { ...rest, date: new Date(date) };
}

/**
 * The link behind a download. The response is always an attachment: these are user-supplied
 * bytes served from the cockpit's own origin, so rendering them in-origin would be the
 * stored-XSS hazard the backend exists to refuse.
 */
export function downloadURL(id: string): string {
  return `${base}/direct?id=${encodeURIComponent(id)}`;
}

/**
 * Stores one file in the folder being viewed.
 *
 * There is no ingestion call after this and there should not be: the sidecar reconciles the
 * bucket, so landing in it IS what gets the file extracted, carded and indexed. The upload
 * therefore finishes long before the file becomes searchable.
 */
export async function uploadFile(parent: string, file: File): Promise<void> {
  const body = new FormData();
  body.append('file', file, file.name);
  const res = await fetch(`${base}/upload?id=${encodeURIComponent(parent)}`, {
    method: 'POST',
    credentials: 'same-origin',
    body,
  });
  if (!res.ok) throw new Error(`HTTP ${String(res.status)}`);
}
