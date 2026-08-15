import { afterEach, describe, expect, it, vi } from 'vitest';
import { createFileManagerProvider, parseDates, directURL } from './filesApi';

/**
 * Captures what the provider actually put on the wire. The header is the whole subject
 * here, so nothing is asserted through a mock of our own code -- fetch is the real
 * boundary, and these are the exact arguments the browser would have received.
 */
function captureFetch(): { init: RequestInit | undefined } {
  const seen = { init: undefined as RequestInit | undefined };
  vi.stubGlobal(
    'fetch',
    vi.fn((_url: string, init?: RequestInit) => {
      seen.init = init;
      // An array satisfies both shapes the provider reads back: loadFiles hands the body
      // to parseDates, and the write handlers under test here return it untouched.
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve([]),
      } as unknown as Response);
    }),
  );
  return seen;
}

function headerOf(init: RequestInit | undefined): string | undefined {
  return (init?.headers as Record<string, string> | undefined)?.['Content-Type'];
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('file manager provider', () => {
  /**
   * The server refuses a write whose body is not labelled application/json, which is the
   * CSRF floor: a cross-origin form can post text/plain without a preflight and cannot
   * post JSON. RestDataProvider.send overrides Rest.sendRequest and drops its
   * "application/json" default, so an unwrapped provider sends no Content-Type at all and
   * the browser stamps text/plain on the body.
   */
  it('labels a JSON body so the server will accept the write', async () => {
    const seen = captureFetch();
    const provider = createFileManagerProvider();

    await provider.send('files', 'DELETE', JSON.stringify({ ids: ['/a.txt'] }));

    expect(headerOf(seen.init)).toBe('application/json');
  });

  it('labels the writes the component itself issues', async () => {
    const seen = captureFetch();
    const provider = createFileManagerProvider();
    const handler = provider.getHandlers()['delete-files']?.handler;
    if (!handler) throw new Error('the provider no longer exposes a delete-files handler');

    await handler({ ids: ['/a.txt'] }, 'delete-files', {});

    expect(seen.init?.method).toBe('DELETE');
    expect(headerOf(seen.init)).toBe('application/json');
  });

  /**
   * An upload is multipart, and only the browser can write its boundary parameter. Setting
   * Content-Type here would produce a boundary-less header and the server would fail to
   * parse a body that is perfectly well formed.
   */
  it('leaves an upload unlabelled so the browser can set the multipart boundary', async () => {
    const seen = captureFetch();
    const provider = createFileManagerProvider();

    await provider.send('upload?id=%2F', 'POST', new FormData());

    expect(headerOf(seen.init)).toBeUndefined();
  });

  it('leaves a bodyless read unlabelled', async () => {
    const seen = captureFetch();
    const provider = createFileManagerProvider();

    await provider.loadFiles('');

    expect(seen.init?.method).toBe('GET');
    expect(headerOf(seen.init)).toBeUndefined();
  });

  it('lets an explicit header win over the default', async () => {
    const seen = captureFetch();
    const provider = createFileManagerProvider();

    await provider.send('files', 'DELETE', '{}', { 'Content-Type': 'application/problem+json' });

    expect(headerOf(seen.init)).toBe('application/problem+json');
  });
});

describe('wire helpers', () => {
  it('turns the wire date string into a Date the widget can sort on', () => {
    const entries = parseDates([
      { id: '/a.txt', name: 'a.txt', date: '2026-08-13T10:00:00Z' } as never,
    ]);
    expect(entries[0]?.date).toBeInstanceOf(Date);
  });

  it('leaves an entry whose date is already a Date alone', () => {
    const when = new Date('2026-08-13T10:00:00Z');
    const entries = parseDates([{ id: '/a.txt', name: 'a.txt', date: when }]);
    expect(entries[0]?.date).toBe(when);
  });

  it('encodes the id and asks for a download only when told to', () => {
    expect(directURL('/conta bilita/a.txt', false)).toBe(
      '/api/filemanager/direct?id=%2Fconta%20bilita%2Fa.txt',
    );
    expect(directURL('/a.txt', true)).toBe('/api/filemanager/direct?id=%2Fa.txt&download=true');
  });
});
