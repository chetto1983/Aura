import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { attachmentPollDelayMs, useAttachmentUploads } from '../useAttachmentUploads';

const presignedAsset = {
  id: 'asset-1',
  status: 'presigned',
  modality: 'document',
  file_name: 'manual.pdf',
  mime_type: 'application/pdf',
  declared_size_bytes: 9,
  size_bytes: 0,
};

const acceptedAsset = { ...presignedAsset, status: 'accepted', size_bytes: 9 };
const searchableAsset = {
  ...presignedAsset,
  status: 'searchable',
  size_bytes: 9,
  document_id: 'doc-1',
};

class FakeXHR {
  static instances: FakeXHR[] = [];
  static errorOnSend = false;
  static lengthComputable = true;
  static nextStatus = 200;
  static throwHeader: string | undefined;
  static throwHeaderWithError = false;

  readonly upload: { onprogress: ((event: ProgressEvent) => void) | null } = { onprogress: null };
  readonly headers: Record<string, string> = {};
  method = '';
  url = '';
  status = FakeXHR.nextStatus;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor() {
    FakeXHR.instances.push(this);
  }

  open(method: string, url: string): void {
    this.method = method;
    this.url = url;
  }

  setRequestHeader(key: string, value: string): void {
    if (FakeXHR.throwHeader?.toLowerCase() === key.toLowerCase()) {
      if (FakeXHR.throwHeaderWithError) throw new Error('header denied');
      throwNonError('header denied');
    }
    this.headers[key] = value;
  }

  send(): void {
    queueMicrotask(() => {
      if (FakeXHR.errorOnSend) {
        this.onerror?.();
        return;
      }
      this.upload.onprogress?.({
        lengthComputable: FakeXHR.lengthComputable,
        loaded: 5,
        total: 10,
      } as ProgressEvent);
      this.onload?.();
    });
  }
}

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function presignResponse({
  asset = presignedAsset,
  headers = {},
}: {
  readonly asset?: typeof presignedAsset;
  readonly headers?: Record<string, string>;
} = {}): Response {
  return jsonResponse({
    asset,
    upload: {
      upload_url: 'https://assets.test/upload',
      method: 'PUT',
      required_headers: headers,
      expires_at: '2026-06-18T20:00:00Z',
    },
  });
}

function makeLocalIdSequence(): () => string {
  let id = 0;
  return () => `local-${String(++id)}`;
}

describe('useAttachmentUploads', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
    FakeXHR.instances = [];
    FakeXHR.errorOnSend = false;
    FakeXHR.lengthComputable = true;
    FakeXHR.nextStatus = 200;
    FakeXHR.throwHeader = undefined;
    FakeXHR.throwHeaderWithError = false;
  });

  it('presigns, uploads with browser-safe headers, finalizes, and polls until ready', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        presignResponse({
          headers: {
            'Content-Type': 'application/pdf',
            'Content-Length': '9',
            Cookie: 'session=forbidden',
            'Proxy-Authorization': 'forbidden',
            'Sec-Fetch-Site': 'same-origin',
            'X-Aura-Test': 'ok',
          },
        }),
      )
      .mockResolvedValueOnce(jsonResponse(acceptedAsset))
      .mockResolvedValueOnce(jsonResponse(searchableAsset));
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['%PDF test'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('ready');
    });

    expect(result.current.readyAssetIds).toEqual(['asset-1']);
    expect(result.current.hasBlockingUploads).toBe(false);
    const calls = fetchMock.mock.calls as unknown as [string, RequestInit?][];
    expect(calls.map(([url]) => url)).toEqual([
      '/api/assets/presign',
      '/api/assets/asset-1/finalize',
      '/api/assets/asset-1',
    ]);
    const xhr = FakeXHR.instances[0];
    expect(xhr?.method).toBe('PUT');
    expect(xhr?.url).toBe('https://assets.test/upload');
    expect(xhr?.headers).toMatchObject({
      'Content-Type': 'application/pdf',
      'X-Aura-Test': 'ok',
    });
    expect(xhr?.headers).not.toHaveProperty('Content-Length');
    expect(xhr?.headers).not.toHaveProperty('Cookie');
    expect(xhr?.headers).not.toHaveProperty('Proxy-Authorization');
    expect(xhr?.headers).not.toHaveProperty('Sec-Fetch-Site');
  });

  it('marks the item failed when finalize fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(presignResponse())
        .mockResolvedValueOnce(new Response('finalize failed', { status: 400 })),
    );
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['x'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('failed');
    });
    expect(result.current.items[0]?.error).toBe('finalize failed');
    expect(result.current.readyAssetIds).toEqual([]);
  });

  it.each([
    ['refused', 'refused'],
    ['failed', 'failed'],
    ['deleted', 'failed'],
    ['canceled', 'failed'],
  ] as const)('maps finalized %s assets to upload status %s', async (assetStatus, itemStatus) => {
    const finalAsset = {
      ...presignedAsset,
      status: assetStatus,
      error_message: `${assetStatus} by processor`,
    };
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(presignResponse())
        .mockResolvedValueOnce(jsonResponse(finalAsset)),
    );
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['x'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe(itemStatus);
    });
    expect(result.current.items[0]?.error).toBe(`${assetStatus} by processor`);
  });

  it('infers modality hints for common asset file types', async () => {
    const files = [
      new File(['png'], 'image.png', { type: 'image/png' }),
      new File(['ogg'], 'voice.ogg', { type: 'audio/ogg' }),
      new File(['pdf'], 'manual.pdf', { type: '' }),
      new File(['docx'], 'manual.docx', { type: '' }),
      new File(['pptx'], 'deck.pptx', { type: '' }),
      new File(['xlsx'], 'sheet.xlsx', { type: '' }),
      new File(['xlsm'], 'macro.xlsm', { type: '' }),
      new File(['html'], 'page.html', { type: '' }),
      new File(['csv'], 'data.csv', { type: '' }),
      new File(['md'], 'notes.md', { type: '' }),
      new File(['txt'], 'notes.txt', { type: '' }),
      new File(['json'], 'data.json', { type: '' }),
      new File(['xml'], 'data.xml', { type: '' }),
      new File(['epub'], 'book.epub', { type: '' }),
    ];
    const finalAssets = [
      { ...presignedAsset, id: 'image-1', modality: 'image', status: 'complete' },
      { ...presignedAsset, id: 'audio-1', modality: 'audio', status: 'complete' },
      { ...searchableAsset, id: 'pdf-1' },
      { ...searchableAsset, id: 'docx-1' },
      { ...searchableAsset, id: 'pptx-1' },
      { ...searchableAsset, id: 'xlsx-1' },
      { ...searchableAsset, id: 'xlsm-1' },
      { ...searchableAsset, id: 'html-1' },
      { ...searchableAsset, id: 'csv-1' },
      { ...searchableAsset, id: 'md-1' },
      { ...searchableAsset, id: 'txt-1' },
      { ...searchableAsset, id: 'json-1' },
      { ...searchableAsset, id: 'xml-1' },
      { ...searchableAsset, id: 'epub-1' },
    ];
    const fetchMock = vi.fn();
    for (const index of finalAssets.keys()) {
      fetchMock.mockResolvedValueOnce(
        presignResponse({ asset: { ...presignedAsset, id: `asset-${String(index)}` } }),
      );
    }
    for (const asset of finalAssets) {
      fetchMock.mockResolvedValueOnce(jsonResponse(asset));
    }
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', {
        pollIntervalMs: 1,
        makeLocalId: makeLocalIdSequence(),
      }),
    );

    act(() => {
      result.current.addFiles(files);
    });

    await waitFor(() => {
      expect(result.current.items.every((item) => item.status === 'ready')).toBe(true);
    });

    const presignBodies = (fetchMock.mock.calls as unknown as [string, RequestInit?][])
      .filter(([url]) => url === '/api/assets/presign')
      .map(([, init]) => {
        const body = init?.body;
        if (typeof body !== 'string') throw new Error('presign body must be JSON');
        return JSON.parse(body) as { modality_hint: string; mime_type: string };
      });
    expect(presignBodies.map((body) => body.modality_hint)).toEqual([
      'image',
      'audio',
      'document',
      'document',
      'document',
      'document',
      'document',
      'document',
      'document',
      'document',
      'document',
      'document',
      'document',
      'document',
    ]);
    expect(presignBodies.at(-1)?.mime_type).toBe('application/octet-stream');
  });

  it('fails when upload responds with a non-2xx status', async () => {
    FakeXHR.nextStatus = 500;
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(presignResponse({ headers: { 'Content-Type': 'application/pdf' } })),
    );
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['x'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('failed');
    });
    expect(result.current.items[0]?.error).toBe('upload failed: HTTP 500');
  });

  it('fails when upload emits a network error', async () => {
    FakeXHR.errorOnSend = true;
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(presignResponse()));
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['x'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('failed');
    });
    expect(result.current.items[0]?.error).toBe('upload failed');
  });

  it('fails when the browser rejects an allowed upload header', async () => {
    FakeXHR.throwHeader = 'X-Aura-Test';
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(presignResponse({ headers: { 'X-Aura-Test': 'ok' } })),
    );
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['x'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('failed');
    });
    expect(result.current.items[0]?.error).toBe('header denied');
  });

  it('reports Error upload-header rejections without wrapping them', async () => {
    FakeXHR.throwHeader = 'X-Aura-Test';
    FakeXHR.throwHeaderWithError = true;
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(presignResponse({ headers: { 'X-Aura-Test': 'ok' } })),
    );
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['x'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('failed');
    });
    expect(result.current.items[0]?.error).toBe('header denied');
  });

  it('ignores upload progress events without computable lengths', async () => {
    FakeXHR.lengthComputable = false;
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(presignResponse())
        .mockResolvedValueOnce(jsonResponse(searchableAsset)),
    );
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['%PDF test'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('ready');
    });
    expect(result.current.items[0]?.progress).toBe(1);
  });

  it('uses crypto ids by default and reports non-Error failures generically', async () => {
    vi.stubGlobal('crypto', { randomUUID: () => 'uuid-1' });
    vi.stubGlobal('fetch', vi.fn().mockRejectedValueOnce('offline'));
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() => useAttachmentUploads('thread-1'));

    act(() => {
      result.current.addFiles([new File(['x'], 'notes.txt')]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('failed');
    });
    expect(result.current.items[0]?.localId).toBe('uuid-1');
    expect(result.current.items[0]?.error).toBe('upload failed');
  });

  it('removes ready uploads, deletes their assets, and clears ready items', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(presignResponse())
      .mockResolvedValueOnce(jsonResponse(searchableAsset))
      .mockResolvedValueOnce(jsonResponse({ ...searchableAsset, status: 'deleted' }));
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['%PDF test'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('ready');
    });

    act(() => {
      result.current.remove('local-1');
    });
    expect(result.current.items).toEqual([]);
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/assets/asset-1',
        expect.objectContaining({ method: 'DELETE' }),
      );
    });

    fetchMock
      .mockResolvedValueOnce(presignResponse())
      .mockResolvedValueOnce(jsonResponse(searchableAsset));
    act(() => {
      result.current.addFiles([new File(['%PDF test'], 'manual.pdf', { type: 'application/pdf' })]);
    });
    await waitFor(() => {
      expect(result.current.items[0]?.status).toBe('ready');
    });
    act(() => {
      result.current.clearReady();
    });
    expect(result.current.items).toEqual([]);
  });

  it('removes queued uploads without issuing asset deletes before presign resolves', () => {
    const fetchMock = vi.fn().mockReturnValue(new Promise(() => undefined));
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { pollIntervalMs: 1, makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['x'], 'manual.pdf', { type: 'application/pdf' })]);
    });
    act(() => {
      result.current.remove('local-1');
    });

    expect(result.current.items).toEqual([]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('uses bounded default polling delays', () => {
    expect(attachmentPollDelayMs(0)).toBe(500);
    expect(attachmentPollDelayMs(4_999)).toBe(500);
    expect(attachmentPollDelayMs(5_000)).toBe(1_500);
    expect(attachmentPollDelayMs(59_999)).toBe(1_500);
    expect(attachmentPollDelayMs(60_000)).toBe(5_000);
  });

  it('waits for the first default poll interval before re-reading an accepted asset', async () => {
    vi.useFakeTimers();
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            asset: presignedAsset,
            upload: {
              upload_url: 'https://assets.test/upload',
              method: 'PUT',
              required_headers: {},
              expires_at: '2026-06-18T20:00:00Z',
            },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(acceptedAsset), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(searchableAsset), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('XMLHttpRequest', FakeXHR);

    const { result } = renderHook(() =>
      useAttachmentUploads('thread-1', { makeLocalId: () => 'local-1' }),
    );

    act(() => {
      result.current.addFiles([new File(['%PDF test'], 'manual.pdf', { type: 'application/pdf' })]);
    });

    await act(async () => {
      vi.runAllTicks();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
    });
    expect(result.current.items[0]?.status).toBe('processing');
    expect(fetchMock).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(499);
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    await vi.waitFor(() => {
      expect(result.current.items[0]?.status).toBe('ready');
    });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });
});

function throwNonError(value: unknown): never {
  throw value;
}
