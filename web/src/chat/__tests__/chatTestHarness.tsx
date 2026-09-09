import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';

// chatTestHarness holds the wire + render helpers shared by the ExternalStoreChat suites.
// Extracted when the error-path tests moved into their own file (CLAUDE.md's 600-LOC cap):
// duplicating an SSE body builder across two suites is exactly how two suites end up
// disagreeing about the wire shape they are both meant to pin.

/** Build one SSE wire body from raw AG-UI frame objects — the same shapes the Go translator
 * emits (internal/agui/translator.go). Used to stub /agent/run for the streaming-path tests. */
export function sseBody(frames: readonly Record<string, unknown>[]): string {
  return frames.map((f) => `event: ${String(f.type)}\ndata: ${JSON.stringify(f)}\n\n`).join('');
}

export function sseResponse(frames: readonly Record<string, unknown>[]): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(sseBody(frames)));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

export function messagesSnapshotResponse(messages: readonly Record<string, unknown>[]): Response {
  return new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

export function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

export function isHistoryURL(url: unknown): boolean {
  return typeof url === 'string' && url.startsWith('/threads/');
}

export function sendPrompt(text: string): void {
  const input = screen.getByPlaceholderText('Ask Aura');
  fireEvent.change(input, { target: { value: text } });
  // Enter submits the composer (Shift+Enter would newline).
  fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
}

export function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

/** renderChatWithClient exposes the QueryClient so a test can spy on the React Query cache
 * invalidation that fires once a turn finishes (AC-5). */
export function renderChatWithClient(ui: ReactElement): { client: QueryClient } {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
  return { client };
}
