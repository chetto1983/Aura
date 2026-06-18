import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from '../ExternalStoreChat';
import { CONVERSATION_KEY } from '../../conversations/useConversations';

// Build a single SSE wire body from raw AG-UI frame objects (the same shapes the
// Go translator emits). Used to stub /agent/run for the streaming-path tests.
function sseBody(frames: readonly Record<string, unknown>[]): string {
  return frames.map((f) => `event: ${String(f.type)}\ndata: ${JSON.stringify(f)}\n\n`).join('');
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

function sendPrompt(text: string): void {
  const input = screen.getByPlaceholderText('Ask Aura');
  fireEvent.change(input, { target: { value: text } });
  // Enter submits the composer (Shift+Enter would newline).
  fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

// renderChatWithClient exposes the QueryClient so a test can spy on the React
// Query cache invalidation that fires once a turn finishes (AC-5).
function renderChatWithClient(ui: ReactElement): { client: QueryClient } {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
  return { client };
}

describe('ExternalStoreChat (CHAT-01)', () => {
  beforeEach(() => {
    localStorage.removeItem('aura.chat.reasoning.shown');
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.removeItem('aura.chat.reasoning.shown');
  });

  it('renders the empty-thread state before any turn', () => {
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    expect(screen.getByText('Type a prompt below to start this run.')).toBeTruthy();
    expect(screen.getByPlaceholderText('Ask Aura')).toBeTruthy();
  });

  it('streams an assistant answer with reasoning drawer + tool card over /agent/run', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        sseResponse([
          { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-1' },
          { type: 'REASONING_START', messageId: 'rsn-1' },
          { type: 'REASONING_MESSAGE_CONTENT', messageId: 'rsn-1', delta: 'let me think' },
          { type: 'REASONING_END', messageId: 'rsn-1' },
          { type: 'TOOL_CALL_START', toolCallId: 'call-1', toolCallName: 'web_search' },
          { type: 'TOOL_CALL_ARGS', toolCallId: 'call-1', delta: '{"q":"meteo"}' },
          { type: 'TOOL_CALL_END', toolCallId: 'call-1' },
          { type: 'TOOL_CALL_RESULT', toolCallId: 'call-1', content: 'sunny 25C' },
          { type: 'TEXT_MESSAGE_START', messageId: 'msg-1' },
          { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-1', delta: 'It is sunny.' },
          { type: 'TEXT_MESSAGE_END', messageId: 'msg-1' },
          {
            type: 'STATE_DELTA',
            delta: [
              { op: 'replace', path: '/prompt_tokens', value: 120 },
              { op: 'replace', path: '/cost_usd', value: 0.001 },
            ],
          },
          {
            type: 'RUN_FINISHED',
            threadId: 'conv-1',
            runId: 'run-1',
            outcome: { type: 'success' },
          },
        ]),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const usageSpy = vi.fn();
    renderChat(<ExternalStoreChat threadId="conv-1" onUsage={usageSpy} />);
    sendPrompt('weather?');

    // The assistant text renders (markdown).
    await waitFor(() => {
      expect(screen.getByText('It is sunny.')).toBeTruthy();
    });

    // POST went to /agent/run with the AG-UI body.
    const call = fetchMock.mock.calls[0];
    expect(call).toBeDefined();
    expect((call as unknown as [string])[0]).toBe('/agent/run');

    // Reasoning drawer rendered the CoT (shown by default).
    expect(screen.getByText('let me think')).toBeTruthy();

    // Tool card shows the tool name + done status; expanding reveals the raw blob.
    expect(screen.getByText('web_search')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Show raw result' }));
    expect(screen.getByText('sunny 25C')).toBeTruthy();

    // Usage flowed to the footer seam (25-04).
    await waitFor(() => {
      expect(usageSpy).toHaveBeenCalled();
    });
    const lastUsage = usageSpy.mock.calls.at(-1)?.[0] as { promptTokens: number } | undefined;
    expect(lastUsage?.promptTokens).toBe(120);
  });

  it('creates a thread id before the first send when no conversation is active', async () => {
    const runBodies: unknown[] = [];
    const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
      if (init?.body !== undefined) {
        if (typeof init.body !== 'string') throw new Error('expected JSON request body');
        runBodies.push(JSON.parse(init.body) as unknown);
      }
      return Promise.resolve(
        sseResponse([
          { type: 'RUN_STARTED', threadId: 'conv-created', runId: 'run-1' },
          { type: 'TEXT_MESSAGE_START', messageId: 'msg-1' },
          { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-1', delta: 'Ciao.' },
          { type: 'TEXT_MESSAGE_END', messageId: 'msg-1' },
          {
            type: 'RUN_FINISHED',
            threadId: 'conv-created',
            runId: 'run-1',
            outcome: { type: 'success' },
          },
        ]),
      );
    });
    vi.stubGlobal('fetch', fetchMock);
    const ensureThread = vi.fn(() => Promise.resolve('conv-created'));

    renderChat(<ExternalStoreChat threadId="" onEnsureThread={ensureThread} />);
    sendPrompt('ciao');

    await waitFor(() => {
      expect(screen.getByText('Ciao.')).toBeTruthy();
    });
    expect(ensureThread).toHaveBeenCalledWith('ciao');
    expect(runBodies[0]).toMatchObject({ threadId: 'conv-created' });
  });

  it('shows the Stop control while running and cancelling aborts the fetch', async () => {
    // A fetch that rejects with AbortError once the signal fires (never resolves
    // otherwise), so the turn stays "running" until cancelled.
    const fetchMock = vi.fn((_url: string, init: RequestInit) => {
      return new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener('abort', () => {
          reject(new DOMException('aborted', 'AbortError'));
        });
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('long task');

    // Stop replaces Send while the turn is in flight.
    const stop = await screen.findByRole('button', { name: 'Stop the current response' });
    expect(stop).toBeTruthy();

    fireEvent.click(stop);

    // After abort the turn ends → Send returns.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Send message' })).toBeTruthy();
    });
    // The fetch carried an AbortSignal that was triggered.
    const init = (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1];
    expect(init.signal).toBeInstanceOf(AbortSignal);
  });

  // AC-5: once a turn completes (RUN_FINISHED → onNew finally), the conversation
  // React-Query read is invalidated so the persisted Session totals refresh.
  it('invalidates the conversation query key when a turn completes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          sseResponse([
            { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-1' },
            { type: 'TEXT_MESSAGE_START', messageId: 'msg-1' },
            { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-1', delta: 'Done.' },
            { type: 'TEXT_MESSAGE_END', messageId: 'msg-1' },
            {
              type: 'STATE_DELTA',
              delta: [{ op: 'replace', path: '/prompt_tokens', value: 42 }],
            },
            {
              type: 'RUN_FINISHED',
              threadId: 'conv-1',
              runId: 'run-1',
              outcome: { type: 'success' },
            },
          ]),
        ),
      ),
    );

    const { client } = renderChatWithClient(<ExternalStoreChat threadId="conv-1" />);
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');
    sendPrompt('any task');

    await waitFor(() => {
      expect(screen.getByText('Done.')).toBeTruthy();
    });

    // The conversation aggregate read for THIS thread is invalidated once the
    // turn settles (the persisted Session seed refreshes).
    await waitFor(() => {
      const invalidatedConversation = invalidateSpy.mock.calls.some(
        (call) =>
          Array.isArray((call[0] as { queryKey?: unknown[] } | undefined)?.queryKey) &&
          (call[0] as { queryKey: unknown[] }).queryKey[0] === CONVERSATION_KEY &&
          (call[0] as { queryKey: unknown[] }).queryKey[1] === 'conv-1',
      );
      expect(invalidatedConversation).toBe(true);
    });
  });

  it('surfaces an incomplete turn when the stream errors', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          sseResponse([
            { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-1' },
            { type: 'RUN_ERROR', message: 'upstream 5xx' },
          ]),
        ),
      ),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('boom');
    // The reducer routes RUN_ERROR into an error text part rendered as markdown.
    await waitFor(() => {
      expect(screen.getByText('upstream 5xx')).toBeTruthy();
    });
  });
});
