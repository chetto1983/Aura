import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useAttachmentUploads } from '../useAttachmentUploads';

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

  readonly upload: { onprogress: ((event: ProgressEvent) => void) | null } = { onprogress: null };
  readonly headers: Record<string, string> = {};
  method = '';
  url = '';
  status = 200;
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
    this.headers[key] = value;
  }

  send(): void {
    queueMicrotask(() => {
      this.upload.onprogress?.({ lengthComputable: true, loaded: 5, total: 10 } as ProgressEvent);
      this.onload?.();
    });
  }
}

describe('useAttachmentUploads', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    FakeXHR.instances = [];
  });

  it('presigns, uploads with required headers, finalizes, and polls until ready', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            asset: presignedAsset,
            upload: {
              upload_url: 'https://assets.test/upload',
              method: 'PUT',
              required_headers: {
                'Content-Type': 'application/pdf',
                'Content-Length': '9',
              },
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
      'Content-Length': '9',
    });
  });

  it('marks the item failed when finalize fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi
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
});
