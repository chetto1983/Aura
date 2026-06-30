import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '../../i18n/i18n';
import DocumentsWorkspace from '../DocumentsWorkspace';

const DOC = {
  id: 'doc-1',
  identity_id: 'operator-1',
  scope: 'library',
  title: 'Handbook.pdf',
  tags: ['ops', 'runbook'],
  metadata: { owner: 'platform' },
  active_version_id: 'ver-2',
  status: 'ready',
  created_at: '2026-06-18T10:00:00Z',
  updated_at: '2026-06-19T10:00:00Z',
};

const DETAIL = {
  document: DOC,
  versions: [
    {
      id: 'ver-2',
      document_id: 'doc-1',
      asset_id: 'asset-2',
      version_number: 2,
      status: 'ready',
      sha256: 'sha256-current',
      content_type: 'application/pdf',
      size_bytes: 2048,
      storage_object_id: 'store-2',
      chunking_config_hash: 'chunk-v1',
      pipeline_config_hash: 'pipe-v2',
      created_at: '2026-06-19T10:00:00Z',
    },
    {
      id: 'ver-1',
      document_id: 'doc-1',
      asset_id: 'asset-1',
      version_number: 1,
      status: 'ready',
      sha256: 'sha256-old',
      content_type: 'application/pdf',
      size_bytes: 1024,
      storage_object_id: 'store-1',
      created_at: '2026-06-18T10:00:00Z',
    },
  ],
};

describe('DocumentsWorkspace', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('loads the catalog, opens versions, saves tags, and deletes through confirmation', async () => {
    const calls: { url: string; init: RequestInit | undefined }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        calls.push({ url, init });
        if (url.startsWith('/api/documents/doc-1') && init?.method === 'PATCH') {
          return Promise.resolve(new Response(JSON.stringify({ ...DOC, tags: ['audit', 'ops'] })));
        }
        if (url.startsWith('/api/documents/doc-1') && init?.method === 'DELETE') {
          return Promise.resolve(new Response(JSON.stringify({ ...DOC, status: 'deleted' })));
        }
        if (url === '/api/documents/doc-1/events') {
          return Promise.resolve(
            new Response(
              JSON.stringify([
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
                  created_at: '2026-06-19T10:01:00Z',
                },
              ]),
            ),
          );
        }
        if (url === '/api/documents/doc-1') {
          return Promise.resolve(new Response(JSON.stringify(DETAIL)));
        }
        if (url.startsWith('/api/documents')) {
          return Promise.resolve(new Response(JSON.stringify([DOC])));
        }
        return Promise.resolve(new Response('{"ok":true}', { status: 200 }));
      }),
    );

    render(<DocumentsWorkspace />);

    expect(await screen.findByRole('heading', { name: 'Document library' })).toBeTruthy();
    expect(screen.getByRole('searchbox', { name: 'Search documents' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'All' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Documents' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Images' })).toBeTruthy();
    expect(await screen.findByRole('row', { name: /Handbook\.pdf.*ready.*2 KB/i })).toBeTruthy();

    fireEvent.click(await screen.findByRole('button', { name: /^Handbook\.pdf/ }));
    const drawer = await screen.findByRole('dialog', { name: 'Handbook.pdf' });
    expect(within(drawer).getByText('Versions')).toBeTruthy();
    expect(within(drawer).getAllByText('sha256-current').length).toBeGreaterThan(0);
    expect(within(drawer).getAllByText('application/pdf').length).toBeGreaterThan(0);
    expect(within(drawer).getAllByText('2 KB').length).toBeGreaterThan(0);
    expect(await within(drawer).findByText('ingestion_job.succeeded')).toBeTruthy();
    expect(within(drawer).getByText('indexed')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Handbook.pdf' }));
    const menu = await screen.findByRole('menu', { name: 'Actions for Handbook.pdf' });
    expect(within(menu).getByRole('menuitem', { name: 'Edit tags' })).toBeTruthy();
    expect(within(menu).getByRole('menuitem', { name: 'Delete document' })).toBeTruthy();

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search documents' }), {
      target: { value: 'handbook' },
    });
    fireEvent.change(screen.getByLabelText('Tag filter'), { target: { value: 'ops' } });
    fireEvent.click(screen.getByRole('button', { name: 'Search' }));

    await waitFor(() => {
      expect(calls.some((call) => call.url === '/api/documents?q=handbook&tag=ops&limit=50')).toBe(
        true,
      );
    });

    fireEvent.change(within(drawer).getByLabelText('Document tags'), {
      target: { value: 'audit, ops' },
    });
    fireEvent.click(within(drawer).getByRole('button', { name: 'Save tags' }));

    await waitFor(() => {
      const patch = calls.find((call) => call.init?.method === 'PATCH');
      expect(patch?.url).toBe('/api/documents/doc-1');
      expect(patch?.init?.body).toBe(
        JSON.stringify({
          title: 'Handbook.pdf',
          tags: ['audit', 'ops'],
          scope: 'library',
          status: 'ready',
          active_version_id: 'ver-2',
          metadata: { owner: 'platform' },
        }),
      );
    });

    fireEvent.click(within(drawer).getByRole('button', { name: 'Delete document' }));
    const dialog = await screen.findByRole('dialog', { name: 'Delete Handbook.pdf' });
    fireEvent.change(within(dialog).getByLabelText('Type DELETE to confirm'), {
      target: { value: 'DELETE' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete permanently' }));

    await waitFor(() => {
      expect(
        calls.some((call) => call.url === '/api/documents/doc-1' && call.init?.method === 'DELETE'),
      ).toBe(true);
    });
  });

  it('dry-runs and deletes storage orphans through typed confirmation', async () => {
    const calls: { url: string; init: RequestInit | undefined }[] = [];
    const report = {
      bucket: 'assets',
      prefix: 'identity/operator-1/',
      dry_run_token: 'token-1',
      deleted: 0,
      orphans: [
        {
          Ref: { Bucket: 'assets', Key: 'identity/operator-1/orphan.pdf' },
          Attrs: { SizeBytes: 4096, ETag: 'etag-1', MIMEType: 'application/pdf' },
        },
      ],
    };
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        calls.push({ url, init });
        if (url.startsWith('/api/storage/orphans/cleanup')) {
          return Promise.resolve(
            new Response(JSON.stringify({ ...report, deleted: 1 }), { status: 200 }),
          );
        }
        if (url.startsWith('/api/storage/orphans')) {
          return Promise.resolve(new Response(JSON.stringify(report), { status: 200 }));
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      }),
    );

    render(<DocumentsWorkspace />);

    expect(screen.queryByLabelText('Storage bucket')).toBeNull();
    fireEvent.click(await screen.findByRole('button', { name: 'Advanced document maintenance' }));
    fireEvent.change(await screen.findByLabelText('Storage bucket'), {
      target: { value: 'assets' },
    });
    fireEvent.change(screen.getByLabelText('Object prefix'), {
      target: { value: 'identity/operator-1/' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Dry run' }));

    expect(await screen.findByText('identity/operator-1/orphan.pdf')).toBeTruthy();
    expect(screen.getByText('4 KB')).toBeTruthy();

    fireEvent.change(screen.getByLabelText('Type DELETE ORPHAN OBJECTS to confirm'), {
      target: { value: 'DELETE ORPHAN OBJECTS' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Delete orphans' }));

    await waitFor(() => {
      const cleanup = calls.find((call) => call.url === '/api/storage/orphans/cleanup');
      expect(cleanup?.init?.method).toBe('POST');
      expect(cleanup?.init?.body).toBe(
        JSON.stringify({
          bucket: 'assets',
          prefix: 'identity/operator-1/',
          dry_run_token: 'token-1',
          confirm: 'DELETE ORPHAN OBJECTS',
        }),
      );
    });
    expect(await screen.findByText('Deleted 1 orphan object')).toBeTruthy();
  });

  it('retries a failed document through its active version asset', async () => {
    const failedDoc = { ...DOC, status: 'failed' };
    const failedDetail = {
      document: failedDoc,
      versions: [{ ...DETAIL.versions[0], asset_id: 'asset-2', status: 'failed' }],
    };
    const calls: { url: string; init: RequestInit | undefined }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        calls.push({ url, init });
        if (url === '/api/assets/asset-2/retry') {
          return Promise.resolve(new Response('{"status":"accepted"}', { status: 200 }));
        }
        if (url === '/api/documents/doc-1/events') {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        if (url === '/api/documents/doc-1') {
          return Promise.resolve(new Response(JSON.stringify(failedDetail), { status: 200 }));
        }
        if (url.startsWith('/api/documents')) {
          return Promise.resolve(new Response(JSON.stringify([failedDoc]), { status: 200 }));
        }
        return Promise.resolve(new Response('{"ok":true}', { status: 200 }));
      }),
    );

    render(<DocumentsWorkspace />);

    fireEvent.click(await screen.findByRole('button', { name: 'Actions for Handbook.pdf' }));
    const menu = await screen.findByRole('menu', { name: 'Actions for Handbook.pdf' });
    fireEvent.click(within(menu).getByRole('menuitem', { name: 'Retry processing' }));

    await waitFor(() => {
      expect(
        calls.some(
          (call) => call.url === '/api/assets/asset-2/retry' && call.init?.method === 'POST',
        ),
      ).toBe(true);
    });
  });
});
