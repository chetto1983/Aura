import type { IEntity } from '@svar-ui/react-filemanager';

/**
 * The mount the file manager talks to. It implements the component's own REST dialect --
 * the same one its official Go reference backend serves -- so RestDataProvider drives every
 * read and every write with no request-building code here.
 */
export const fileManagerBase = '/api/filemanager';

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
