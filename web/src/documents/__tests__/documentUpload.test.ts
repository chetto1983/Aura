import { afterEach, describe, expect, it, vi } from 'vitest';
import { uploadLibraryDocument, waitForDocumentIngestion } from '../documentUpload';
import type { Asset } from '../../chat/attachments/types';

function assetResponse(status: string, extra: Record<string, unknown> = {}): Response {
  return new Response(
    JSON.stringify({
      id: 'asset-1',
      status,
      modality: 'document',
      file_name: 'Manual.pdf',
      mime_type: 'application/pdf',
      declared_size_bytes: 10,
      size_bytes: 10,
      ...extra,
    }),
  );
}

function presignResponse(): Response {
  return new Response(
    JSON.stringify({
      asset: {
        id: 'asset-1',
        status: 'presigned',
        modality: 'document',
        file_name: 'Manual.pdf',
        mime_type: 'application/pdf',
        declared_size_bytes: 10,
        size_bytes: 0,
      },
      upload: {
        upload_url: 'https://assets.test/upload',
        method: 'PUT',
        required_headers: {},
        expires_at: '2026-06-30T00:00:00Z',
      },
    }),
  );
}

class FakeXHR {
  static instances: FakeXHR[] = [];
  readonly upload = { onprogress: null as ((event: ProgressEvent) => void) | null };
  status = 200;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  method = '';
  url = '';
  body: BodyInit | null = null;

  constructor() {
    FakeXHR.instances.push(this);
  }

  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }

  setRequestHeader() {
    return undefined;
  }

  send(body: BodyInit | null) {
    this.body = body;
    this.upload.onprogress?.({ lengthComputable: true, loaded: 5, total: 10 } as ProgressEvent);
    this.onload?.();
  }
}

describe('uploadLibraryDocument', () => {
  afterEach(() => {
    FakeXHR.instances = [];
    vi.unstubAllGlobals();
  });

  it('presigns as library scope, uploads, finalizes, and reports progress', async () => {
    const progress: number[] = [];
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(presignResponse())
        .mockResolvedValueOnce(assetResponse('processing', { document_id: 'doc-1' })),
    );

    const file = new File(['0123456789'], 'Manual.pdf', { type: 'application/pdf' });
    const asset = await uploadLibraryDocument(file, (value) => progress.push(value));

    expect(asset.document_id).toBe('doc-1');
    expect(progress).toContain(0.5);
    const fetchMock = vi.mocked(fetch);
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toMatchObject({
      scope: 'library',
      thread_id: '',
      file_name: 'Manual.pdf',
      modality_hint: 'document',
    });
    expect(FakeXHR.instances[0]?.method).toBe('PUT');
  });

  it('resolves while the asset is still processing instead of waiting for ingestion', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(presignResponse())
      .mockResolvedValueOnce(assetResponse('processing'));
    vi.stubGlobal('fetch', fetchMock);

    const file = new File(['0123456789'], 'Manual.pdf', { type: 'application/pdf' });
    const asset = await uploadLibraryDocument(file);

    expect(asset.status).toBe('processing');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('rejects when the server refuses the finalized upload', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(presignResponse())
        .mockResolvedValueOnce(assetResponse('failed', { error_message: 'processor failed' })),
    );

    const file = new File(['0123456789'], 'Manual.pdf', { type: 'application/pdf' });
    await expect(uploadLibraryDocument(file)).rejects.toThrow('processor failed');
  });
});

describe('waitForDocumentIngestion', () => {
  const processing: Asset = {
    id: 'asset-1',
    status: 'processing',
    modality: 'document',
    file_name: 'Manual.pdf',
    mime_type: 'application/pdf',
    declared_size_bytes: 10,
    size_bytes: 10,
  };

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('polls until the asset is searchable', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(assetResponse('processing'))
      .mockResolvedValueOnce(assetResponse('searchable', { document_id: 'doc-1' }));
    vi.stubGlobal('fetch', fetchMock);

    const settled = await waitForDocumentIngestion(processing, { pollIntervalMs: 0 });

    expect(settled.status).toBe('searchable');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('returns a failed asset instead of throwing', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(assetResponse('failed')));

    const settled = await waitForDocumentIngestion(processing, { pollIntervalMs: 0 });

    expect(settled.status).toBe('failed');
  });

  it('stops polling once aborted', async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn().mockImplementation(() => {
      controller.abort();
      return Promise.resolve(assetResponse('processing'));
    });
    vi.stubGlobal('fetch', fetchMock);

    const settled = await waitForDocumentIngestion(processing, {
      pollIntervalMs: 0,
      signal: controller.signal,
    });

    expect(settled.status).toBe('processing');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('returns immediately when the asset already settled', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const settled = await waitForDocumentIngestion({ ...processing, status: 'complete' });

    expect(settled.status).toBe('complete');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
