import type { IEntity } from '@svar-ui/react-filemanager';
import { RestDataProvider } from '@svar-ui/filemanager-data-provider';

/**
 * The mount the file manager talks to. It implements the component's own REST dialect --
 * the same one its official Go reference backend serves -- so RestDataProvider drives every
 * read and every write with no request-building code here.
 */
export const fileManagerBase = '/api/filemanager';

/**
 * The provider, with the one thing its author left out put back.
 *
 * RestDataProvider.send overrides Rest.sendRequest and loses that method's
 * `"Content-Type": "application/json"` default, so every write it issues leaves the header
 * unset and the browser stamps the JSON string text/plain. The server refuses that, and it
 * is right to: a cross-origin form can post text/plain with no preflight but cannot post
 * application/json, so the header check is what makes the browser ask permission before a
 * page the operator never opened can rename or delete their files. The server-side hole
 * that used to paper over this is closed -- the label belongs on the request.
 *
 * customHeaders is the library's own seam (the fourth argument of send), so nothing here
 * reimplements or forks the provider's request building.
 */
class LabelledRestDataProvider extends RestDataProvider {
  override send<T>(
    url: string,
    method: string,
    data?: unknown,
    customHeaders?: Record<string, string>,
  ): Promise<T> {
    // An upload is multipart and only the browser can write its boundary parameter, so
    // labelling that one would break the body it is meant to describe.
    const carriesJSON = data != null && !(data instanceof FormData);
    return super.send<T>(
      url,
      method,
      data,
      carriesJSON ? { 'Content-Type': 'application/json', ...customHeaders } : customHeaders,
    );
  }
}

export function createFileManagerProvider(): RestDataProvider {
  return new LabelledRestDataProvider(fileManagerBase);
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
 * Opening renders in a new tab. That is safe because the response carries a sandbox
 * Content-Security-Policy: the document lands in an opaque origin with no scripts and no
 * same-origin access, so user-supplied bytes cannot reach the cockpit's session.
 */
export function directURL(id: string, download: boolean): string {
  const query = `id=${encodeURIComponent(id)}${download ? '&download=true' : ''}`;
  return `${fileManagerBase}/direct?${query}`;
}
