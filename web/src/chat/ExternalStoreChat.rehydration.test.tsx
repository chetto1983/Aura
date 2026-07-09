import type { ReactElement } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat, foldAgentOntoAssistant } from './ExternalStoreChat';
import type { Asset } from './attachments/types';

// 37B plan 05 — D-15 split-fold rehydration + onArtifact forwarding.
//   1. foldAgentOntoAssistant (pure) attributes agent deliverables to ASSISTANT
//      turns and NEVER to user turns (the exact bug being fixed).
//   2. On saved-conversation load, agent assets rehydrate the durable authenticated
//      download chip on their assistant message while user uploads keep their
//      user-turn card (non-regression).
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

function userTurn(id: string, text: string): ThreadMessageLike {
  return { id, role: 'user', content: [{ type: 'text', text }] };
}

function assistantTurn(id: string, text: string): ThreadMessageLike {
  return { id, role: 'assistant', content: [{ type: 'text', text }] };
}

describe('foldAgentOntoAssistant (D-15 attribution)', () => {
  it('attaches an agent asset to the assistant turn and NEVER to the user turn', () => {
    const messages = [userTurn('m1', 'make me a spreadsheet'), assistantTurn('m2', 'here it is')];
    const folded = foldAgentOntoAssistant(messages, [agentAsset()]);

    const user = folded[0];
    const assistant = folded[1];
    if (user === undefined || assistant === undefined) throw new Error('expected two turns');

    // The user turn is untouched (the regression assertion on the bug).
    const userAttachments = (user.metadata?.custom as { attachments?: Asset[] } | undefined)
      ?.attachments;
    expect(userAttachments).toBeUndefined();

    // The agent asset lands on the assistant turn.
    const assistantAttachments = (
      assistant.metadata?.custom as { attachments?: Asset[] } | undefined
    )?.attachments;
    expect(assistantAttachments).toEqual([agentAsset()]);
  });

  it('drops deleted/canceled agent assets and leaves messages untouched when none remain', () => {
    const messages = [userTurn('m1', 'hi'), assistantTurn('m2', 'ok')];
    const folded = foldAgentOntoAssistant(messages, [agentAsset({ status: 'deleted' })]);
    expect(folded[1]?.metadata).toBeUndefined();
  });

  it('returns messages unchanged when there is no assistant turn to fold onto', () => {
    const messages = [userTurn('m1', 'hi')];
    const folded = foldAgentOntoAssistant(messages, [agentAsset()]);
    expect(folded[0]?.metadata).toBeUndefined();
  });
});

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

describe('ExternalStoreChat rehydration (D-15) + onArtifact', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('rehydrates the agent download chip on the assistant turn and keeps uploads on the user turn', async () => {
    const fetchMock = vi.fn((url: unknown) => {
      if (url === '/threads/conv-1/messages') {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              type: 'MESSAGES_SNAPSHOT',
              messages: [
                { id: 'msg-1', role: 'user', content: 'make me a spreadsheet' },
                { id: 'msg-2', role: 'assistant', content: 'here it is' },
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

    // The agent deliverable renders the authenticated download anchor (D-15 chip).
    const chip = await screen.findByRole('link', { name: /report\.xlsx/i });
    expect(chip.getAttribute('href')).toBe('/api/assets/ag-1/download');

    // The user upload keeps its user-turn card (non-regression) — and it is NOT a
    // download anchor, so the agent asset never leaked onto the user turn.
    expect(screen.getAllByText('upload.pdf').length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByRole('link', { name: /upload\.pdf/i })).toBeNull();
    // Exactly one download anchor exists (the agent chip on the assistant turn).
    expect(screen.getAllByRole('link')).toHaveLength(1);
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
