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
    expect(await screen.findByRole('button', { name: /Handbook\.pdf/ })).toBeTruthy();
    expect((await screen.findAllByText('sha256-current')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('application/pdf').length).toBeGreaterThan(0);
    expect(screen.getAllByText('2 KB').length).toBeGreaterThan(0);

    fireEvent.change(screen.getByLabelText('Search documents'), {
      target: { value: 'handbook' },
    });
    fireEvent.change(screen.getByLabelText('Tag filter'), { target: { value: 'ops' } });
    fireEvent.click(screen.getByRole('button', { name: 'Search' }));

    await waitFor(() => {
      expect(calls.some((call) => call.url === '/api/documents?q=handbook&tag=ops&limit=50')).toBe(
        true,
      );
    });

    fireEvent.change(screen.getByLabelText('Document tags'), {
      target: { value: 'audit, ops' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save tags' }));

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

    fireEvent.click(screen.getByRole('button', { name: 'Delete document' }));
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
});
