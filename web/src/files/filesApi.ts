import type { IEntity } from '@svar-ui/react-filemanager';
import { RestDataProvider } from '@svar-ui/filemanager-data-provider';
import { httpErrorFrom } from '@/api/json';
import { downloadLinks } from '@/chat/artifacts/downloadAll';
import type { AssetSource } from '@/chat/artifacts/renderers/assetSourceContext';

/**
 * The mount the file manager talks to. It implements the component's own REST dialect --
 * the same one its official Go reference backend serves -- so RestDataProvider drives every
 * read and every write with no request-building code here.
 */
export const fileManagerBase = '/api/filemanager';

/**
 * The provider, with the two things its author left out put back.
 *
 * The label. RestDataProvider.send overrides Rest.sendRequest and loses that method's
 * `"Content-Type": "application/json"` default, so every write it issues leaves the header
 * unset and the browser stamps the JSON string text/plain. The server refuses that, and it
 * is right to: a cross-origin form can post text/plain with no preflight but cannot post
 * application/json, so the header check is what makes the browser ask permission before a
 * page the operator never opened can rename or delete their files.
 *
 * The failure. Its send logs a refused request to the console and resolves undefined, so a
 * write the server stopped part way reads as done to the widget, which has already changed
 * its view. Measured on 2026-10-18: a delete failed, the folder looked emptied, and the
 * operator saw no word. Here a refusal rejects, with the server's own message, and a write's
 * refusal also reaches every onWriteFailed listener, so the page can say so and show what is
 * stored.
 */
export class FileManagerProvider extends RestDataProvider {
  private readonly writeListeners = new Set<(message: string) => void>();

  /** Subscribes to refused writes; the returned function unsubscribes. */
  onWriteFailed(listener: (message: string) => void): () => void {
    this.writeListeners.add(listener);
    return () => {
      this.writeListeners.delete(listener);
    };
  }

  override async send<T>(
    url: string,
    method: string,
    data?: unknown,
    customHeaders?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<T> {
    // An upload is multipart and only the browser can write its boundary parameter, so
    // labelling that one would break the body it is meant to describe.
    const carriesJSON = data != null && !(data instanceof FormData);
    const response = await fetch(`${this._url}/${url}`, {
      method,
      ...(signal ? { signal } : {}),
      headers: carriesJSON
        ? { 'Content-Type': 'application/json', ...customHeaders }
        : { ...customHeaders },
      body: (data ?? null) as BodyInit | null,
    });
    const body = (await response.json().catch(() => ({}))) as { error?: string };
    if (response.ok) return body as T;
    const message = body.error ?? `HTTP ${String(response.status)}`;
    if (method !== 'GET') {
      for (const listener of this.writeListeners) listener(message);
    }
    throw new Error(message);
  }
}

export function createFileManagerProvider(): FileManagerProvider {
  return new FileManagerProvider(fileManagerBase);
}

/**
 * Turns the wire's date strings into Dates, which is what the widget sorts and formats on.
 * The entity type is the component's own -- declaring a parallel one here would be a second
 * definition of the same contract, free to drift from it.
 */
export function parseDates(entries: readonly IEntity[]): IEntity[] {
  return entries.map((entry) =>
    typeof entry.date === 'string' ? { ...entry, date: new Date(entry.date as string) } : entry,
  );
}

/**
 * The link behind opening or downloading a file.
 *
 * Opening renders in a new tab. That is safe because the server renders inline only the types
 * that run no script (inlineSafeMIME in internal/agui/files_api.go) and serves every other one
 * as an attachment, so user-supplied bytes cannot reach the cockpit's session.
 */
export function directURL(id: string, download: boolean): string {
  const query = `id=${encodeURIComponent(id)}${download ? '&download=true' : ''}`;
  return `${fileManagerBase}/direct?${query}`;
}

/**
 * Where the cockpit's preview reads a file manager object from: the direct route, inline. That
 * route answers Range requests itself, so a clip streams from the same URL; there is no render
 * route and no editing, so those optional capabilities are left out.
 */
export const FILE_MANAGER_SOURCE: AssetSource = {
  assetUrl: (id) => directURL(id, false),
  streamUrl: (id) => directURL(id, false),
  credentials: 'same-origin',
  save: (id, bytes, saveAs) => saveBeside(id, bytes, saveAs),
};

/**
 * saveBeside writes what a Craft app saved (CraftFrame) into the folder of the file manager
 * object `id`: over that object itself when `saveAs` is absent, which keeps the name it is
 * known by, or as a new file named `saveAs` beside it (Save As, a PDF export). It goes through
 * the upload route, so the ingest reads it again like any upload.
 */
export async function saveBeside(id: string, bytes: ArrayBuffer, saveAs?: string): Promise<void> {
  const folder = id.slice(0, id.lastIndexOf('/') + 1);
  const form = new FormData();
  form.append('file', new File([bytes], saveAs ?? id.slice(folder.length)));
  const res = await fetch(`${fileManagerBase}/upload?id=${encodeURIComponent(folder)}`, {
    method: 'POST',
    credentials: 'same-origin',
    body: form,
  });
  if (!res.ok) throw await httpErrorFrom(res);
}

/**
 * Saves files through the cockpit's one multi-download loop -- the one "Download all" uses,
 * paced against Chromium's multi-download burst block -- so one file and many take the same
 * route.
 */
export function saveFiles(ids: readonly string[]): Promise<void> {
  return downloadLinks(ids.map((id) => ({ href: directURL(id, true), fileName: '' })));
}
