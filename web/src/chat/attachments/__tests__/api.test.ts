import { afterEach, describe, expect, it, vi } from 'vitest';
import { deleteAsset, finalizeAsset, getAsset, presignAsset, promoteAsset } from '../api';

const asset = {
  id: 'asset-1',
  status: 'searchable',
  modality: 'document',
  file_name: 'manual.pdf',
  mime_type: 'application/pdf',
  declared_size_bytes: 9,
  size_bytes: 9,
};

describe('attachment API client', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('surfaces a non-OK backend body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('asset refused', { status: 400 }))),
    );

    await expect(getAsset('asset-1')).rejects.toThrow('asset refused');
  });

  it('falls back to HTTP status when a non-OK body is empty', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 503 }))),
    );

    await expect(getAsset('asset-1')).rejects.toThrow('HTTP 503');
  });

  it('uses same-origin JSON requests for asset mutations', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ asset, upload: { upload_url: '/put', method: 'PUT' } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    await presignAsset({
      thread_id: 'thread-1',
      file_name: 'manual.pdf',
      mime_type: 'application/pdf',
      size_bytes: 9,
      modality_hint: 'document',
    });

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/assets/presign',
      expect.objectContaining({
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({
          thread_id: 'thread-1',
          file_name: 'manual.pdf',
          mime_type: 'application/pdf',
          size_bytes: 9,
          modality_hint: 'document',
        }),
      }),
    );
  });

  it('encodes ids for asset routes', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify(asset), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    await finalizeAsset('asset/1');
    await promoteAsset('asset/1');
    await deleteAsset('asset/1');

    const calls = fetchMock.mock.calls as unknown as [string, RequestInit][];
    expect(calls.map((call) => call[0])).toEqual([
      '/api/assets/asset%2F1/finalize',
      '/api/assets/asset%2F1/promote',
      '/api/assets/asset%2F1',
    ]);
  });
});
