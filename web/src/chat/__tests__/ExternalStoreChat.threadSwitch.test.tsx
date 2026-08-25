import type { ReactElement } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function messagesSnapshot(threadId: string): Response {
  return jsonResponse({
    type: 'MESSAGES_SNAPSHOT',
    messages: [
      {
        id: `history-${threadId}`,
        role: 'user',
        content: `Persisted ${threadId}`,
      },
    ],
  });
}

function conversation(threadId: string): Response {
  return jsonResponse({
    ID: threadId,
    Title: threadId,
    TitleSet: true,
    IdentityID: 'operator',
    Status: 'active',
    Model: 'test',
    TotalInputTokens: 0,
    TotalOutputTokens: 0,
    TotalCachedTokens: 0,
    TotalCostUSD: 0,
    CreatedAt: '2026-08-25T00:00:00Z',
  });
}

function sseWire(frames: readonly Record<string, unknown>[], firstId = 1): string {
  return frames
    .map(
      (frame, index) =>
        `event: ${String(frame.type)}\nid: ${String(firstId + index)}\ndata: ${JSON.stringify(frame)}\n\n`,
    )
    .join('');
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rendered = render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
  return {
    ...rendered,
    rerenderChat: (next: ReactElement) => {
      rendered.rerender(<QueryClientProvider client={client}>{next}</QueryClientProvider>);
    },
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ExternalStoreChat thread ownership', () => {
  it('detaches A and hydrates B when the thread changes during a live run', async () => {
    let runSignal: AbortSignal | undefined;
    let runBody: { threadId?: string } | undefined;
    let runStream: ReadableStreamDefaultController<Uint8Array> | undefined;
    const historyReads: string[] = [];
    const encoder = new TextEncoder();

    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = requestURL(input);
        if (url.startsWith('/threads/') && url.endsWith('/messages')) {
          const threadId = url.split('/')[2] ?? '';
          historyReads.push(threadId);
          return Promise.resolve(messagesSnapshot(threadId));
        }
        if (url.startsWith('/api/conversations/')) {
          return Promise.resolve(conversation(url.slice('/api/conversations/'.length)));
        }
        if (url === '/agent/run') {
          runSignal = init?.signal ?? undefined;
          if (typeof init?.body === 'string') {
            runBody = JSON.parse(init.body) as { threadId?: string };
          }
          const body = new ReadableStream<Uint8Array>({
            start(controller) {
              runStream = controller;
              controller.enqueue(
                encoder.encode(
                  sseWire([{ type: 'RUN_STARTED', threadId: 'conv-a', runId: 'run-a' }]),
                ),
              );
            },
          });
          return Promise.resolve(
            new Response(body, {
              status: 200,
              headers: { 'Content-Type': 'text/event-stream' },
            }),
          );
        }
        return Promise.resolve(jsonResponse([]));
      }),
    );

    const { rerenderChat } = renderChat(<ExternalStoreChat threadId="conv-a" />);
    expect(await screen.findByText('Persisted conv-a')).toBeTruthy();

    const composer = screen.getByPlaceholderText('Ask Aura');
    fireEvent.change(composer, { target: { value: 'Prompt for A' } });
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' });
    await waitFor(() => {
      expect(runStream).toBeDefined();
    });

    rerenderChat(<ExternalStoreChat threadId="conv-b" />);
    expect(await screen.findByText('Persisted conv-b')).toBeTruthy();
    expect(runBody?.threadId).toBe('conv-a');
    expect(historyReads).toContain('conv-b');
    expect(runSignal?.aborted).toBe(true);

    await act(async () => {
      runStream?.enqueue(
        encoder.encode(
          sseWire(
            [
              { type: 'TEXT_MESSAGE_START', messageId: 'answer-a' },
              { type: 'TEXT_MESSAGE_CONTENT', messageId: 'answer-a', delta: 'Answer from A' },
              { type: 'TEXT_MESSAGE_END', messageId: 'answer-a' },
              {
                type: 'RUN_FINISHED',
                threadId: 'conv-a',
                runId: 'run-a',
                outcome: { type: 'success' },
              },
            ],
            2,
          ),
        ),
      );
      runStream?.close();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(screen.queryByText('Persisted conv-a')).toBeNull();
    expect(screen.queryByText('Prompt for A')).toBeNull();
    expect(screen.queryByText('Answer from A')).toBeNull();
  });
});
