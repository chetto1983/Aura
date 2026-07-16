import { useState, type ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import type { RunUsageEvent } from '../runUsage';

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function messagesSnapshotResponse(): Response {
  return jsonResponse({ type: 'MESSAGES_SNAPSHOT', messages: [] });
}

function sseResponse(frames: readonly Record<string, unknown>[]): Response {
  const encoder = new TextEncoder();
  const wire = frames
    .map((frame) => `event: ${String(frame.type)}\ndata: ${JSON.stringify(frame)}\n\n`)
    .join('');
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoder.encode(wire));
        controller.close();
      },
    }),
    { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
  );
}

function completedRun(threadId: string): Response {
  return sseResponse([
    { type: 'RUN_STARTED', threadId, runId: 'backend-run' },
    { type: 'TEXT_MESSAGE_START', messageId: 'answer' },
    { type: 'TEXT_MESSAGE_CONTENT', messageId: 'answer', delta: 'Done.' },
    { type: 'TEXT_MESSAGE_END', messageId: 'answer' },
    {
      type: 'STATE_DELTA',
      delta: [{ op: 'replace', path: '/prompt_tokens', value: 42 }],
    },
    { type: 'RUN_FINISHED', threadId, runId: 'backend-run', outcome: { type: 'success' } },
  ]);
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

function eventsOf(spy: ReturnType<typeof vi.fn>): RunUsageEvent[] {
  return spy.mock.calls.map(([event]) => event as RunUsageEvent);
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe('ExternalStoreChat usage ownership races', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.removeItem('aura.chat.reasoning.shown');
  });

  it('cancels before first-thread creation resolves without issuing the agent run', async () => {
    const created = deferred<string>();
    const onUsage = vi.fn();
    const runRequests: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url === '/agent/run') {
          runRequests.push(url);
          return Promise.resolve(completedRun('conv-created'));
        }
        return Promise.resolve(jsonResponse([]));
      }),
    );

    renderChat(
      <ExternalStoreChat threadId="" onEnsureThread={() => created.promise} onUsage={onUsage} />,
    );
    sendPrompt('cancel during creation');
    fireEvent.click(await screen.findByRole('button', { name: 'Stop the current response' }));

    await act(async () => {
      created.resolve('conv-created');
      await created.promise;
    });
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Send message' })).toBeTruthy();
    });

    expect(runRequests).toHaveLength(0);
    const events = eventsOf(onUsage);
    const runId = events.find((event) => event.phase === 'running')?.runId;
    expect(runId).toBeDefined();
    expect(
      events.filter((event) => event.runId === runId && event.phase === 'settled'),
    ).toHaveLength(1);
  });

  it('keeps an immediately settled first send through matching thread adoption', async () => {
    const onUsage = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url === '/agent/run') return Promise.resolve(completedRun('conv-created'));
        if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
        return Promise.resolve(jsonResponse([]));
      }),
    );

    function FirstSendHost() {
      const [threadId, setThreadId] = useState('');
      return (
        <ExternalStoreChat
          threadId={threadId}
          onEnsureThread={(text) => {
            expect(text).toBe('fast first send');
            setThreadId('conv-created');
            return Promise.resolve('conv-created');
          }}
          onUsage={onUsage}
        />
      );
    }

    renderChat(<FirstSendHost />);
    sendPrompt('fast first send');
    await screen.findByText('Done.');
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    const events = eventsOf(onUsage);
    const startIndex = events.findIndex((event) => event.phase === 'running');
    const owned = events.slice(startIndex);
    expect(owned.at(-1)).toMatchObject({
      phase: 'settled',
      usage: { promptTokens: 42 },
    });
    expect(owned.some((event) => event.phase === 'idle')).toBe(false);
  });

  it('aborts an active stream when the chat surface unmounts', async () => {
    let runSignal: AbortSignal | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
        if (url === '/agent/run') {
          runSignal = init?.signal ?? undefined;
          return new Promise<Response>((_resolve, reject) => {
            runSignal?.addEventListener('abort', () => {
              reject(new DOMException('aborted', 'AbortError'));
            });
          });
        }
        return Promise.resolve(jsonResponse([]));
      }),
    );

    const view = renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('keep running');
    await waitFor(() => {
      expect(runSignal).toBeDefined();
    });

    view.unmount();
    expect(runSignal?.aborted).toBe(true);
  });
});
