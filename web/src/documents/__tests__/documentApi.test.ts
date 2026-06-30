import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  cleanupStorageOrphans,
  deleteDocument,
  fetchDocumentDetail,
  fetchDocumentEvents,
  fetchDocuments,
  fetchStorageOrphans,
  updateDocument,
} from '../documentApi';

describe('documentApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists and reads documents with encoded filters using same-origin credentials', async () => {
    const listBody = [
      {
        id: 'doc-1',
        identity_id: 'operator-1',
        scope: 'library',
        title: 'Runbook',
        tags: ['ops'],
        metadata: {},
        active_version_id: 'ver-1',
        status: 'ready',
      },
    ];
    const detailBody = { document: listBody[0], versions: [] };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(listBody), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(detailBody), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(
      fetchDocuments({ query: 'policy pdf', tag: 'ops/critical', limit: 25, offset: 50 }),
    ).resolves.toEqual(listBody);
    await expect(fetchDocumentDetail('doc/1')).resolves.toEqual(detailBody);

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/documents?q=policy+pdf&tag=ops%2Fcritical&limit=25&offset=50',
      {
        headers: { Accept: 'application/json' },
        credentials: 'same-origin',
      },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/documents/doc%2F1', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('updates, deletes, dry-runs, and confirms storage cleanup with safe payloads', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response('{"ok":true}', { status: 200 })));
    vi.stubGlobal('fetch', fetchMock);

    await updateDocument('doc-1', {
      title: 'Runbook v2',
      tags: ['ops', 'ready'],
      scope: 'library',
      status: 'ready',
      metadata: { owner: 'platform' },
    });
    await deleteDocument('doc-1');
    await fetchStorageOrphans({ bucket: 'assets', prefix: 'identity/operator-1/' });
    await cleanupStorageOrphans({
      bucket: 'assets',
      prefix: 'identity/operator-1/',
      dry_run_token: 'token',
      confirm: 'DELETE ORPHAN OBJECTS',
    });

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/documents/doc-1', {
      method: 'PATCH',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({
        title: 'Runbook v2',
        tags: ['ops', 'ready'],
        scope: 'library',
        status: 'ready',
        metadata: { owner: 'platform' },
      }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/documents/doc-1', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/storage/orphans?bucket=assets&prefix=identity%2Foperator-1%2F',
      {
        headers: { Accept: 'application/json' },
        credentials: 'same-origin',
      },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/storage/orphans/cleanup', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({
        bucket: 'assets',
        prefix: 'identity/operator-1/',
        dry_run_token: 'token',
        confirm: 'DELETE ORPHAN OBJECTS',
      }),
    });
  });

  it('reads document ingestion events from the timeline endpoint', async () => {
    const body = [
      {
        id: 10,
        entity_type: 'ingestion_job',
        entity_id: 'job-1',
        job_id: 'job-1',
        from_status: 'running',
        to_status: 'succeeded',
        event_type: 'ingestion_job.succeeded',
        message: 'indexed',
        detail: { stage: 'embedding' },
      },
    ];
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify(body), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(fetchDocumentEvents('doc/1')).resolves.toEqual(body);
    expect(fetchMock).toHaveBeenCalledWith('/api/documents/doc%2F1/events', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });
});
