import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from '../ExternalStoreChat';

// RS-07 component-level coverage: the §4.2 reload-attach (live_run_id →
// GET /agent/runs/{id}/events full replay) and the §4.4 Stop → cancel POST.
// Wire fixtures carry `id:` lines — the flag-on gateway shape.

function sseBody(frames: readonly Record<string, unknown>[]): string {
  return frames
    .map((f, i) => `event: ${String(f.type)}\nid: ${String(i + 1)}\ndata: ${JSON.stringify(f)}\n\n`)
    .join('');
}

function sseResponse(frames: readonly Record<string, unknown>[]): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(sseBody(frames)));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function messagesSnapshotResponse(messages: readonly Record<string, unknown>[]): Response {
  return new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function sendPrompt(text: string): void {
  const input = screen.getByPlaceholderText('Ask Aura');
  fireEvent.change(input, { target: { value: text } });
  fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
}

const LIVE_CONVERSATION = {
  ID: 'conv-1',
  Title: '',
  TitleSet: false,
  IdentityID: 'id-1',
  Status: 'active',
  Model: 'test-model',
  TotalInputTokens: 0,
  TotalOutputTokens: 0,
  TotalCachedTokens: 0,
  TotalCostUSD: 0,
  CreatedAt: '2026-01-01T00:00:00Z',
  live_run_id: 'run-7',
};

describe('ExternalStoreChat — RS-07 live-run attach + Stop→cancel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('attaches to a live run on thread open (live_run_id → full-replay resume GET)', async () => {
    let attachInit: RequestInit | undefined;
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.startsWith('/threads/')) {
        return Promise.resolve(
          messagesSnapshotResponse([{ id: 'msg-1', role: 'user', content: 'earlier prompt' }]),
        );
      }
      if (url === '/api/conversations/conv-1') {
        return Promise.resolve(jsonResponse(LIVE_CONVERSATION));
      }
      if (url === '/agent/runs/run-7/events') {
        attachInit = init;
        return Promise.resolve(
          sseResponse([
            { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-7' },
            { type: 'TEXT_MESSAGE_START', messageId: 'msg-live' },
            { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-live', delta: 'Attached answer.' },
            { type: 'TEXT_MESSAGE_END', messageId: 'msg-live' },
            {
              type: 'RUN_FINISHED',
              threadId: 'conv-1',
              runId: 'run-7',
              outcome: { type: 'success' },
            },
          ]),
        );
      }
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);

    // The run-in-progress hint renders while live_run_id is set (§4.2 step 3).
    expect(
      await screen.findByText(
        'A run is still in progress in this conversation. Sending re-enables when it finishes.',
      ),
    ).toBeTruthy();

    // The snapshot renders first, then the attach folds the replayed turn.
    expect(await screen.findByText('earlier prompt')).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByText('Attached answer.')).toBeTruthy();
    });

    // RUN_FINISHED is authoritative even if the conversation refetch races the
    // registry cleanup and briefly returns the stale live_run_id again. The
    // composer must release as soon as the attached stream is terminal.
    fireEvent.change(screen.getByPlaceholderText('Ask Aura'), {
      target: { value: 'follow-up draft' },
    });
    await waitFor(() => {
      expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Send message' }).disabled).toBe(
        false,
      );
    });
    expect(
      screen.queryByText(
        'A run is still in progress in this conversation. Sending re-enables when it finishes.',
      ),
    ).toBeNull();

    // The attach GET is the read-only resume endpoint with NO Last-Event-ID
    // (full-buffer replay rebuilds the in-flight partial turn).
    if (attachInit === undefined) throw new Error('expected the resume GET');
    expect(attachInit.method).toBe('GET');
    expect(new Headers(attachInit.headers).get('Last-Event-ID')).toBeNull();
  });

  it('Stop aborts the viewer and POSTs /agent/runs/{id}/cancel (§4.4)', async () => {
    let cancelInit: RequestInit | undefined;
    const enc = new TextEncoder();
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse([]));
      if (url === '/agent/run') {
        // The stream reveals the runId then stays open — the detached run
        // outlives the viewer, so only the cancel POST can stop it.
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(
              enc.encode(
                `event: RUN_STARTED\nid: 1\ndata: ${JSON.stringify({
                  type: 'RUN_STARTED',
                  threadId: 'conv-1',
                  runId: 'run-42',
                })}\n\n`,
              ),
            );
          },
        });
        return Promise.resolve(
          new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }),
        );
      }
      if (url === '/agent/runs/run-42/cancel') {
        cancelInit = init;
        return Promise.resolve(new Response('{"status":"cancelling"}', { status: 202 }));
      }
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('long task');

    const stop = await screen.findByRole('button', { name: 'Stop the current response' });
    fireEvent.click(stop);

    await waitFor(() => {
      expect(cancelInit).toBeDefined();
    });
    expect(cancelInit?.method).toBe('POST');
    expect(cancelInit?.credentials).toBe('same-origin');
  });
});
