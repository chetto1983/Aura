import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from './ExternalStoreChat';
import type { Asset } from './attachments/types';

// Saved-conversation rehydration + onArtifact forwarding.
//   1. An agent deliverable renders once, on the send_file call that delivered it: the
//      snapshot carries that call's local_artifact display (migration 0126), and the
//      thread's asset list never folds it onto some assistant turn by position.
//   2. User uploads keep their user-turn card (non-regression).
//   3. ExternalStoreChat forwards its onArtifact prop into the stream so an
//      aura.artifact frame drives the panel signal.

function agentAsset(over: Partial<Asset> = {}): Asset {
  return {
    id: 'ag-1',
    source_kind: 'agent',
    status: 'complete',
    modality: 'document',
    file_name: 'report.xlsx',
    mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    declared_size_bytes: 4096,
    size_bytes: 4096,
    ...over,
  };
}

function uploadAsset(over: Partial<Asset> = {}): Asset {
  return {
    id: 'up-1',
    source_kind: 'web',
    status: 'searchable',
    modality: 'document',
    file_name: 'upload.pdf',
    mime_type: 'application/pdf',
    declared_size_bytes: 9,
    size_bytes: 9,
    ...over,
  };
}

function sseArtifactResponse(assetId: string): Response {
  const enc = new TextEncoder();
  const frames = [
    { type: 'RUN_STARTED' },
    {
      type: 'CUSTOM',
      name: 'aura.artifact',
      value: { tool_call_id: 'call-1', filename: 'report.xlsx', asset_id: assetId },
    },
    { type: 'RUN_FINISHED', outcome: { type: 'success' } },
  ];
  const wire = frames.map((f) => `event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`).join('');
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe('ExternalStoreChat rehydration + onArtifact', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The live defect (2026-09-11, "Ciao, presentazione dell'assistente"): the file a later
  // turn delivered rendered under the greeting.
  it('renders an agent file once, on the call that delivered it', async () => {
    const fetchMock = vi.fn((url: unknown) => {
      if (url === '/threads/conv-1/messages') {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              type: 'MESSAGES_SNAPSHOT',
              messages: [
                { id: 'msg-1', role: 'user', content: 'ciao' },
                { id: 'msg-2', role: 'assistant', content: 'Ciao! Sono Aura.' },
                {
                  id: 'msg-3',
                  role: 'user',
                  content: 'make me a spreadsheet',
                  attachmentIds: ['up-1'],
                },
                {
                  id: 'msg-4',
                  role: 'assistant',
                  toolCalls: [
                    {
                      id: 'call-1',
                      type: 'function',
                      function: {
                        name: 'send_file',
                        arguments: '{"path":"/workspace/report.xlsx"}',
                      },
                      display: {
                        type: 'local_artifact',
                        tool_call_id: 'call-1',
                        artifact: { filename: 'report.xlsx', size_bytes: 4096, asset_id: 'ag-1' },
                      },
                    },
                  ],
                },
                {
                  id: 'msg-5',
                  role: 'tool',
                  toolCallId: 'call-1',
                  content: 'queued report.xlsx for delivery',
                },
                { id: 'msg-6', role: 'assistant', content: 'here it is' },
              ],
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        );
      }
      if (url === '/api/assets?thread_id=conv-1') {
        return Promise.resolve(
          new Response(JSON.stringify([uploadAsset(), agentAsset()]), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      return Promise.resolve(new Response('[]', { status: 200 }));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);

    const chips = await screen.findAllByRole('link', { name: /report\.xlsx/i });
    expect(chips).toHaveLength(1);
    expect(chips[0]?.getAttribute('href')).toBe('/api/assets/ag-1/download');
    const greeting = screen
      .getByText('Ciao! Sono Aura.')
      .closest('[data-message-role="assistant"]');
    expect(greeting?.querySelector('a')).toBeNull();

    // The user upload keeps its user-turn card, and it is not a download anchor.
    expect(screen.getAllByText('upload.pdf').length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByRole('link', { name: /upload\.pdf/i })).toBeNull();
  });

  it('forwards onArtifact into streamRun so an aura.artifact frame fires the signal', async () => {
    const fetchMock = vi.fn((url: unknown) => {
      if (url === '/threads/conv-1/messages') {
        return Promise.resolve(
          new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      if (url === '/api/assets?thread_id=conv-1') {
        return Promise.resolve(new Response('[]', { status: 200 }));
      }
      return Promise.resolve(sseArtifactResponse('asset-9'));
    });
    vi.stubGlobal('fetch', fetchMock);

    const onArtifact = vi.fn();
    renderChat(<ExternalStoreChat threadId="conv-1" onArtifact={onArtifact} />);

    const input = await screen.findByPlaceholderText('Ask Aura');
    fireEvent.change(input, { target: { value: 'ship the file' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });

    await waitFor(() => {
      expect(onArtifact).toHaveBeenCalledWith('asset-9');
    });
  });
});
