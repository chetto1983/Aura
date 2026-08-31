import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';

// The upload lifecycle is an assistant-ui AttachmentAdapter now, so the double is the
// ADAPTER rather than a hook: add() yields a ready attachment immediately and assetFor()
// hands back the Aura asset the optimistic turn renders. The path under test is therefore
// the real one — paste → composer.addAttachment → adapter → send → run envelope.
const READY_ASSET = {
  id: 'asset-1',
  status: 'searchable',
  modality: 'document',
  file_name: 'manual.pdf',
  mime_type: 'application/pdf',
  declared_size_bytes: 9,
  size_bytes: 9,
  document_id: 'doc-1',
  summary: 'indexed',
};

vi.mock('../attachments/auraAttachmentAdapter', () => ({
  createAuraAttachmentAdapter: () => ({
    accept: 'application/pdf',
    // eslint-disable-next-line @typescript-eslint/require-await
    async *add({ file }: { file: File }) {
      yield {
        id: 'asset-1',
        type: 'document',
        name: file.name,
        contentType: file.type,
        file,
        status: { type: 'requires-action', reason: 'composer-send' },
      };
    },
    send: (attachment: unknown) =>
      Promise.resolve({ ...(attachment as object), status: { type: 'complete' }, content: [] }),
    remove: () => Promise.resolve(),
    assetFor: (id: string) => (id === 'asset-1' ? READY_ASSET : undefined),
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
  });

  it('sends the attachment id the adapter produced with the run', async () => {
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

    // The paste is fired at the TEXTAREA, which is where a browser delivers it and where
    // ComposerPrimitive.Input listens for it (addAttachmentOnPaste). It used to be fired at
    // the composer root because Aura had its own handler there — the one that made a single
    // pasted file arrive twice, once from each listener.
    fireEvent.paste(screen.getByPlaceholderText('Ask Aura'), {
      clipboardData: { files: [new File(['x'], 'manual.pdf', { type: 'application/pdf' })] },
    });

    const input = screen.getByPlaceholderText('Ask Aura');
    fireEvent.change(input, { target: { value: 'summarize it' } });
    await waitFor(() => {
      expect(screen.getByText('manual.pdf')).toBeTruthy();
    });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });

    await waitFor(() => {
      const post = fetchMock.mock.calls.find((call) => call[0] === '/agent/run') as
        [string, RequestInit] | undefined;
      expect(post).toBeDefined();
      if (post === undefined) throw new Error('expected /agent/run POST');
      expect(JSON.parse(post[1].body as string)).toMatchObject({
        aura: { attachment_ids: ['asset-1'] },
      });
    });
  });

  it('loads thread assets during replay and renders them beside user messages', async () => {
    const visiblePrompt = 'persisted prompt';
    const fetchMock = vi.fn((url: unknown) => {
      if (url === '/threads/conv-1/messages') {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              type: 'MESSAGES_SNAPSHOT',
              messages: [
                {
                  id: 'msg-1',
                  role: 'user',
                  content:
                    '<knowledge_base trust="operator_pinned_context">\n' +
                    'These documents the user uploaded earlier are indexed.\n' +
                    '- [1] document_id=doc-1 filename=manual.pdf\n' +
                    '</knowledge_base>\n\nUser message:\n' +
                    visiblePrompt,
                },
                { id: 'msg-2', role: 'assistant', content: 'persisted answer' },
              ],
            }),
            {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            },
          ),
        );
      }
      if (url === '/api/assets?thread_id=conv-1') {
        return Promise.resolve(
          new Response(
            JSON.stringify([
              {
                id: 'asset-replay',
                status: 'searchable',
                modality: 'document',
                file_name: 'manual.pdf',
                mime_type: 'application/pdf',
                declared_size_bytes: 9,
                size_bytes: 9,
                document_id: 'doc-1',
                summary: 'indexed on replay',
              },
            ]),
            {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            },
          ),
        );
      }
      return Promise.resolve(sseResponse());
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);

    expect(await screen.findByText(visiblePrompt)).toBeTruthy();
    expect(screen.queryByText(/knowledge_base/)).toBeNull();
    expect(await screen.findByText('indexed on replay')).toBeTruthy();
    expect(screen.getAllByText('manual.pdf').length).toBeGreaterThanOrEqual(1);
  });
});
