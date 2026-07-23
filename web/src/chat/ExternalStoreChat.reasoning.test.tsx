import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from './ExternalStoreChat';

// fix-plan 1.12 — rehydrated reasoning MUST render on saved chats. The server
// persists + serves `reasoning`/`reasoningDurationMs` on answer-shaped assistant
// messages and snapshotToThreadMessages maps them to a {type:'reasoning'} part.
//
// This suite pins the FULL render path (snapshot fetch -> external store runtime
// -> fromThreadMessageLike -> MessagePrimitive.Parts -> reasoning affordance) —
// the level the original 1.12 unit tests did not cover. The operator-reported
// "no reasoning drawer on saved chats" was artifact skew, not a code defect:
// commit 1eeeb74a3 landed the web/src mapping WITHOUT refreshing the baked
// internal/webui/dist, so the deployed cockpit served the pre-1.12 bundle while
// the Go server already emitted `reasoning` (dist refreshed later in 730b1adea).
// A regression at ANY layer of the chain now fails here, in CI, instead of on
// the operator's screen.

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function snapshotFetchMock(messages: unknown[]): (url: unknown) => Promise<Response> {
  return (url: unknown) => {
    if (url === '/threads/conv-1/messages') {
      return Promise.resolve(
        new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }
    return Promise.resolve(new Response('[]', { status: 200 }));
  };
}

const reasoningAffordance = /reasoning|ragionamento|thought for/i;

describe('rehydrated reasoning rendering (fix-plan 1.12)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the reasoning affordance for a snapshot-built assistant message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        snapshotFetchMock([
          { id: 'msg-1', role: 'user', content: 'why is the sky blue' },
          {
            id: 'msg-2',
            role: 'assistant',
            content: 'Rayleigh scattering.',
            reasoning: 'Let me think about light wavelengths and scattering physics.',
            reasoningDurationMs: 46804,
          },
        ]),
      ),
    );

    renderChat(<ExternalStoreChat threadId="conv-1" />);

    await screen.findByText('Rayleigh scattering.');
    expect(await screen.findByRole('button', { name: reasoningAffordance })).toBeTruthy();
  });

  it('renders the reasoning affordance when tool turns precede the answer (live thread shape)', async () => {
    // Mirrors the verified live thread 019f8f37-…: assistant tool-call turns
    // (including a 2-call turn with CONSECUTIVE tool result rows) followed by
    // one answer-shaped assistant message decorated with reasoning.
    vi.stubGlobal(
      'fetch',
      vi.fn(
        snapshotFetchMock([
          { id: 'msg-0', role: 'user', content: 'search the docs then answer' },
          {
            id: 'msg-1',
            role: 'assistant',
            content: '',
            toolCalls: [
              {
                id: 'call-1',
                type: 'function',
                function: { name: 'document_search', arguments: '{"query":"docs"}' },
              },
            ],
          },
          { id: 'msg-2', role: 'tool', toolCallId: 'call-1', content: 'result body' },
          {
            id: 'msg-3',
            role: 'assistant',
            content: '',
            toolCalls: [
              {
                id: 'call-2',
                type: 'function',
                function: { name: 'fs_read', arguments: '{"path":"a.md"}' },
              },
              {
                id: 'call-3',
                type: 'function',
                function: { name: 'fs_read', arguments: '{"path":"b.md"}' },
              },
            ],
          },
          { id: 'msg-4', role: 'tool', toolCallId: 'call-2', content: 'body a' },
          { id: 'msg-5', role: 'tool', toolCallId: 'call-3', content: 'body b' },
          {
            id: 'msg-6',
            role: 'assistant',
            content: 'Final answer after tools.',
            reasoning: 'Chain of thought accumulated over the tool run.',
            reasoningDurationMs: 46804,
          },
        ]),
      ),
    );

    renderChat(<ExternalStoreChat threadId="conv-1" />);

    await screen.findByText('Final answer after tools.');
    expect(await screen.findByRole('button', { name: reasoningAffordance })).toBeTruthy();
  });
});
