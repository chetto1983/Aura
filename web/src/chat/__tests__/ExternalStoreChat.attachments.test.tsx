import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';

const clearReady = vi.hoisted(() => vi.fn());

vi.mock('../attachments/useAttachmentUploads', () => ({
  useAttachmentUploads: () => ({
    items: [
      {
        localId: 'local-1',
        file: { name: 'manual.pdf' },
        asset: {
          id: 'asset-1',
          status: 'searchable',
          modality: 'document',
          file_name: 'manual.pdf',
          mime_type: 'application/pdf',
          declared_size_bytes: 9,
          size_bytes: 9,
          document_id: 'doc-1',
          summary: 'indexed',
        },
        progress: 1,
        status: 'ready',
      },
    ],
    readyAssetIds: ['asset-1'],
    hasBlockingUploads: false,
    addFiles: vi.fn(),
    remove: vi.fn(),
    clearReady,
  }),
}));

function sseResponse(): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode('event: RUN_STARTED\ndata: {"type":"RUN_STARTED"}\n\n'));
      controller.enqueue(
        enc.encode(
          'event: RUN_FINISHED\ndata: {"type":"RUN_FINISHED","outcome":{"type":"success"}}\n\n',
        ),
      );
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe('ExternalStoreChat attachments', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    clearReady.mockClear();
  });

  it('sends ready attachment ids with the run and clears ready chips after success', async () => {
    const fetchMock = vi.fn((url: unknown) => {
      if (typeof url === 'string' && url.startsWith('/threads/')) {
        return Promise.resolve(
          new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      return Promise.resolve(sseResponse());
    });
    vi.stubGlobal('fetch', fetchMock);
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    const input = screen.getByPlaceholderText('Ask Aura');
    fireEvent.change(input, { target: { value: 'summarize it' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });

    await waitFor(() => {
      const post = fetchMock.mock.calls.find((call) => call[0] === '/agent/run') as
        | [string, RequestInit]
        | undefined;
      expect(post).toBeDefined();
      if (post === undefined) throw new Error('expected /agent/run POST');
      const init = post[1];
      expect(JSON.parse(init.body as string)).toMatchObject({
        aura: { attachment_ids: ['asset-1'] },
      });
      expect(clearReady).toHaveBeenCalledTimes(1);
    });
  });
});
