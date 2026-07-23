import type { ReactElement } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import { hasAnswerText } from '../ExternalStoreChat_folds';

// Operator directive: the assistant action bar (Copy / Regenerate / TTS) renders
// ONLY on messages carrying a real answer — at least one non-empty TEXT part.
// Tool-card-only and reasoning-only turns are machinery, not copyable answers.
// The pure predicate is unit-tested; the component suite proves rehydrated
// snapshot messages behave identically to live-built ones (same runtime path).

describe('hasAnswerText (pure predicate)', () => {
  const message = (content: ThreadMessageLike['content']): ThreadMessageLike => ({
    role: 'assistant',
    content,
  });

  it('is false for a tool-call-only message', () => {
    expect(
      hasAnswerText(
        message([{ type: 'tool-call', toolCallId: 'c1', toolName: 'web_search', argsText: '{}' }]),
      ),
    ).toBe(false);
  });

  it('is false for a reasoning-only message', () => {
    expect(hasAnswerText(message([{ type: 'reasoning', text: 'thinking hard' }]))).toBe(false);
  });

  it('is false for an empty or whitespace-only text part', () => {
    expect(hasAnswerText(message([{ type: 'text', text: '' }]))).toBe(false);
    expect(hasAnswerText(message([{ type: 'text', text: '  \n\t ' }]))).toBe(false);
  });

  it('is true when a non-empty text part rides with tool parts', () => {
    expect(
      hasAnswerText(
        message([
          { type: 'tool-call', toolCallId: 'c1', toolName: 'web_search', argsText: '{}' },
          { type: 'text', text: 'Here is the answer.' },
        ]),
      ),
    ).toBe(true);
  });

  it('handles string content', () => {
    expect(hasAnswerText(message('plain answer'))).toBe(true);
    expect(hasAnswerText(message('   '))).toBe(false);
  });
});

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe('assistant action bar gating (rehydrated snapshot)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the bar only under the answer message, never under tool-only turns', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (url === '/threads/conv-1/messages') {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                type: 'MESSAGES_SNAPSHOT',
                messages: [
                  { id: 'msg-0', role: 'user', content: 'run a tool then answer' },
                  {
                    id: 'msg-1',
                    role: 'assistant',
                    content: '',
                    toolCalls: [
                      {
                        id: 'call-1',
                        type: 'function',
                        function: { name: 'web_search', arguments: '{"query":"x"}' },
                      },
                    ],
                  },
                  { id: 'msg-2', role: 'tool', toolCallId: 'call-1', content: 'tool body' },
                  { id: 'msg-3', role: 'assistant', content: 'The final answer.' },
                ],
              }),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      }),
    );

    renderChat(<ExternalStoreChat threadId="conv-1" />);

    await screen.findByText('The final answer.');
    await waitFor(() => {
      const assistantRoots = document.querySelectorAll('[data-message-role="assistant"]');
      expect(assistantRoots.length).toBe(2);
    });

    const assistantRoots = [...document.querySelectorAll('[data-message-role="assistant"]')];
    const toolOnly = assistantRoots[0];
    const answer = assistantRoots[1];
    if (toolOnly === undefined || answer === undefined) throw new Error('expected two turns');

    // The tool-card-only turn has NO action bar; the answer turn has exactly one.
    expect(toolOnly.querySelector('[data-message-actions]')).toBeNull();
    expect(answer.querySelectorAll('[data-message-actions]')).toHaveLength(1);
  });
});
