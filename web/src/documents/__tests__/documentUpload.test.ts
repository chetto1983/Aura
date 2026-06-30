import { afterEach, describe, expect, it, vi } from 'vitest';
import { uploadLibraryDocument } from '../documentUpload';

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

  it('presigns as library scope, uploads, finalizes, polls, and reports progress', async () => {
    const progress: number[] = [];
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          new Response(
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
          ),
        )
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              id: 'asset-1',
              status: 'processing',
              modality: 'document',
              file_name: 'Manual.pdf',
              mime_type: 'application/pdf',
              declared_size_bytes: 10,
              size_bytes: 10,
            }),
          ),
        )
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              id: 'asset-1',
              status: 'searchable',
              modality: 'document',
              file_name: 'Manual.pdf',
              mime_type: 'application/pdf',
              declared_size_bytes: 10,
              size_bytes: 10,
              document_id: 'doc-1',
            }),
          ),
        ),
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

  it('rejects when document processing fails', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          new Response(
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
          ),
        )
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              id: 'asset-1',
              status: 'failed',
              modality: 'document',
              file_name: 'Manual.pdf',
              mime_type: 'application/pdf',
              declared_size_bytes: 10,
              size_bytes: 10,
              error_message: 'processor failed',
            }),
          ),
        ),
    );

    const file = new File(['0123456789'], 'Manual.pdf', { type: 'application/pdf' });
    await expect(uploadLibraryDocument(file)).rejects.toThrow('processor failed');
  });
});
