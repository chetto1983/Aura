import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { Approval } from '../../approvals/useApprovals';
import { ExternalStoreChat } from '../ExternalStoreChat';

function row(token: string, question: string): Approval {
  return {
    token,
    conversation_id: 'c-1',
    kind: 'clarification',
    question,
    options: ['Yes'],
    priority: 0,
  };
}

function json(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function sseResponse(): Response {
  const frames = [
    { type: 'RUN_STARTED', threadId: 'c-1', runId: 'resume-1' },
    {
      type: 'RUN_FINISHED',
      threadId: 'c-1',
      runId: 'resume-1',
      outcome: { type: 'success' },
    },
  ];
  const wire = frames
    .map((frame) => `event: ${frame.type}\ndata: ${JSON.stringify(frame)}\n\n`)
    .join('');
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function historyResponse(): Response {
  return json({ type: 'MESSAGES_SNAPSHOT', messages: [] });
}

function conversationResponse(): Response {
  return json({
    ID: 'c-1',
    Title: 'Approval test',
    TitleSet: true,
    IdentityID: 'operator',
    Status: 'active',
    Model: 'test',
    TotalInputTokens: 0,
    TotalOutputTokens: 0,
    TotalCachedTokens: 0,
    TotalCostUSD: 0,
    CreatedAt: '2026-07-16T00:00:00Z',
    ReasoningEffort: 'auto',
  });
}

function approvalsFetch(
  initial: readonly Approval[],
  runRequests: RequestInit[],
): ReturnType<typeof vi.fn> {
  let pending = [...initial];
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const method = init?.method ?? 'GET';
    if (url === '/api/approvals' && method === 'GET') return Promise.resolve(json(pending));
    if (url.includes('/api/approvals/') && url.endsWith('/resolve') && method === 'POST') {
      const token = decodeURIComponent(url.split('/').at(-2) ?? '');
      const body = JSON.parse(init?.body as string) as { action: string };
      pending =
        body.action === 'cancel' ? [] : pending.filter((approval) => approval.token !== token);
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    if (url === '/agent/run' && method === 'POST') {
      runRequests.push(init ?? {});
      return Promise.resolve(sseResponse());
    }
    if (url === '/threads/c-1/messages') return Promise.resolve(historyResponse());
    if (url.startsWith('/api/assets?')) return Promise.resolve(json([]));
    if (url === '/api/composer/skills') return Promise.resolve(json({ skills: [] }));
    if (url === '/api/composer/reasoning-capabilities') {
      return Promise.resolve(json({ levels: ['auto', 'off'], default: 'auto', detected: false }));
    }
    if (url === '/api/conversations/c-1') return Promise.resolve(conversationResponse());
    return Promise.resolve(json([]));
  });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function first<T>(rows: readonly T[]): T {
  const value = rows[0];
  if (value === undefined) throw new Error('expected at least one row');
  return value;
}

beforeEach(() => {
  localStorage.removeItem('aura.chat.reasoning.shown');
  vi.stubGlobal('CSS', { escape: (value: string) => value });
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.removeItem('aura.chat.reasoning.shown');
});

describe('ExternalStoreChat approval gate', () => {
  it('renders ordered approvals before a locked composer and re-drives exactly once after the third answer', async () => {
    const pending = [
      row('token-1', 'First decision'),
      row('token-2', 'Second decision'),
      row('token-3', 'Third decision'),
    ];
    const runRequests: RequestInit[] = [];
    vi.stubGlobal('fetch', approvalsFetch(pending, runRequests));
    renderChat(<ExternalStoreChat threadId="c-1" />);

    const composer = await screen.findByTestId('chat-composer');
    const cards = await screen.findByTestId('thread-approvals');
    expect(cards.compareDocumentPosition(composer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(composer.getAttribute('aria-disabled')).toBe('true');
    expect(screen.getByPlaceholderText('Ask Aura')).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Add files' })).toHaveProperty('disabled', true);

    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
    await waitFor(() => {
      expect(runRequests).toHaveLength(0);
    });
    await waitFor(() => {
      expect(
        document.activeElement
          ?.closest('[data-approval-token]')
          ?.getAttribute('data-approval-token'),
      ).toBe('token-2');
    });

    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
    await waitFor(() => {
      expect(runRequests).toHaveLength(0);
    });
    await waitFor(() => {
      expect(
        document.activeElement
          ?.closest('[data-approval-token]')
          ?.getAttribute('data-approval-token'),
      ).toBe('token-3');
    });
    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
    await waitFor(() => {
      expect(runRequests).toHaveLength(1);
    });
    expect(screen.getByPlaceholderText('Ask Aura')).toHaveProperty('disabled', false);
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByPlaceholderText('Ask Aura'));
    });
  });

  it('cancels the approval queue without re-driving and restores composer focus', async () => {
    const runRequests: RequestInit[] = [];
    vi.stubGlobal(
      'fetch',
      approvalsFetch(
        [row('token-1', 'First decision'), row('token-2', 'Second decision')],
        runRequests,
      ),
    );
    renderChat(<ExternalStoreChat threadId="c-1" />);

    const composer = await screen.findByTestId('chat-composer');
    expect(composer.getAttribute('aria-disabled')).toBe('true');
    const cancelButtons = await screen.findAllByRole('button', { name: 'Cancel run' });
    fireEvent.click(first(cancelButtons));

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Ask Aura')).toHaveProperty('disabled', false);
    });
    expect(runRequests).toHaveLength(0);
    expect(document.activeElement).toBe(screen.getByPlaceholderText('Ask Aura'));
  });
});
