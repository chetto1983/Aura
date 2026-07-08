import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useBlobPreview } from './useBlobPreview';

// useBlobPreview — the object-URL lifecycle hook behind the image/pdf preview
// renderers (D-06/WEBART-05). It drives one same-origin authenticated fetch,
// relabels the returned octet-stream Blob with the SSE mime_type, and hands out a
// blob: object URL. The load-bearing invariant (Pitfall 5 / T-37B-11) is that the
// URL is revoked AND the fetch aborted in the SAME cleanup keyed on assetId, so a
// rapid open/close (or thread switch) never leaks a blob: entry or flashes a stale
// file. Every branch here is a Stryker mutation target.

type FetchFn = (url: string, init: RequestInit) => Promise<Response>;

const relabeled: Blob[] = [];
let createCount = 0;
let createSpy: ReturnType<typeof vi.fn>;
let revokeSpy: ReturnType<typeof vi.fn>;
let fetchMock: ReturnType<typeof vi.fn<FetchFn>>;
let fetchArgs: { url: string; init: RequestInit }[];

function okResponse(body: Blob): Response {
  return { ok: true, status: 200, blob: () => Promise.resolve(body) } as unknown as Response;
}

function notFound(): Response {
  return {
    ok: false,
    status: 404,
    blob: () => Promise.reject(new Error('blob() must not be read on a non-ok response')),
  } as unknown as Response;
}

/** Index an array under noUncheckedIndexedAccess, failing loudly if the slot is empty. */
function at<T>(arr: readonly T[], i: number): T {
  const value = arr[i];
  if (value === undefined) throw new Error('expected an array element');
  return value;
}

beforeEach(() => {
  relabeled.length = 0;
  createCount = 0;
  fetchArgs = [];
  createSpy = vi.fn((blob: Blob) => {
    relabeled.push(blob);
    createCount += 1;
    return `blob:mock-${String(createCount)}`;
  });
  revokeSpy = vi.fn();
  URL.createObjectURL = createSpy as unknown as typeof URL.createObjectURL;
  URL.revokeObjectURL = revokeSpy as unknown as typeof URL.revokeObjectURL;
  fetchMock = vi.fn<FetchFn>((url, init) => {
    fetchArgs.push({ url, init });
    return Promise.resolve(okResponse(new Blob(['payload'])));
  });
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useBlobPreview', () => {
  it('fetches the asset download route with same-origin credentials and an abort signal', async () => {
    renderHook(() => useBlobPreview('asset-1', 'application/pdf'));
    await waitFor(() => {
      expect(createSpy).toHaveBeenCalled();
    });

    expect(fetchArgs).toHaveLength(1);
    expect(at(fetchArgs, 0).url).toBe('/api/assets/asset-1/download');
    expect(at(fetchArgs, 0).init.credentials).toBe('same-origin');
    expect(at(fetchArgs, 0).init.signal).toBeInstanceOf(AbortSignal);
  });

  it('relabels the fetched Blob with the provided mimeType before creating the URL', async () => {
    const { result } = renderHook(() => useBlobPreview('asset-1', 'application/pdf'));
    await waitFor(() => {
      expect(result.current.url).toBe('blob:mock-1');
    });

    expect(createSpy).toHaveBeenCalledTimes(1);
    expect(at(relabeled, 0).type).toBe('application/pdf');
    expect(result.current.error).toBeUndefined();
  });

  it('leaves the Blob unlabelled when no mimeType is passed', async () => {
    const { result } = renderHook(() => useBlobPreview('asset-1'));
    await waitFor(() => {
      expect(result.current.url).toBe('blob:mock-1');
    });

    // The raw fetched Blob was handed through untouched (its own empty type).
    expect(at(relabeled, 0).type).toBe('');
  });

  it('exposes an error and no url on a non-ok response (blob never read)', async () => {
    fetchMock.mockImplementationOnce((url, init) => {
      fetchArgs.push({ url, init });
      return Promise.resolve(notFound());
    });
    const { result } = renderHook(() => useBlobPreview('asset-1', 'application/pdf'));

    await waitFor(() => {
      expect(result.current.error).toContain('404');
    });
    expect(result.current.url).toBeUndefined();
    expect(createSpy).not.toHaveBeenCalled();
  });

  it('aborts the fetch and revokes the object URL on unmount', async () => {
    const { result, unmount } = renderHook(() => useBlobPreview('asset-1', 'application/pdf'));
    await waitFor(() => {
      expect(result.current.url).toBe('blob:mock-1');
    });

    const signal = at(fetchArgs, 0).init.signal;
    if (!(signal instanceof AbortSignal)) throw new Error('fetch was not given an AbortSignal');
    expect(signal.aborted).toBe(false);

    unmount();

    expect(signal.aborted).toBe(true);
    expect(revokeSpy).toHaveBeenCalledWith('blob:mock-1');
  });

  it('revokes the previous URL and aborts the previous fetch when assetId changes', async () => {
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useBlobPreview(id, 'application/pdf'),
      { initialProps: { id: 'asset-1' } },
    );
    await waitFor(() => {
      expect(result.current.url).toBe('blob:mock-1');
    });
    const firstSignal = at(fetchArgs, 0).init.signal;
    if (!(firstSignal instanceof AbortSignal))
      throw new Error('fetch was not given an AbortSignal');

    rerender({ id: 'asset-2' });
    await waitFor(() => {
      expect(result.current.url).toBe('blob:mock-2');
    });

    expect(firstSignal.aborted).toBe(true);
    expect(revokeSpy).toHaveBeenCalledWith('blob:mock-1');
    expect(at(fetchArgs, 1).url).toBe('/api/assets/asset-2/download');
  });
});
