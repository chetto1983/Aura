import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import '../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from './ExternalStoreChat';
import { useSteerSend } from './ExternalStoreChat_steer';

// ExternalStoreChat_steer — Phase 52 plan 07, Task 2. Two layers:
//
// 1. useSteerSend in isolation (renderHook): the run-id resolution, the optimistic
//    append + rollback, and the onFrame dedup (a tab that both sends and observes shows
//    ONE notice).
// 2. The full ExternalStoreChat component: the behavioural contract no structural change
//    can fake — "a submit while a run is live issues a steer POST and no run POST", and its
//    exact inverse with no live run. This is also the plan's own Step-1 measurement,
//    encoded rather than merely noted: with NO live run resolvable, a submit takes the
//    unchanged /agent/run path (today's behaviour, preserved).

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function messagesSnapshotResponse(messages: readonly Record<string, unknown>[] = []): Response {
  return jsonResponse({ type: 'MESSAGES_SNAPSHOT', messages });
}

/** An SSE stream that starts a run and never closes — the run stays "live" for the
 *  duration of the test, exactly like ExternalStoreChat.liveRun.test.tsx's Stop-aborts
 *  fixture. Steering needs a resolvable run id, which RUN_STARTED supplies. */
function openRunStream(runId: string): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(
        enc.encode(
          `event: RUN_STARTED\nid: 1\ndata: ${JSON.stringify({
            type: 'RUN_STARTED',
            threadId: 'conv-1',
            runId,
          })}\n\n`,
        ),
      );
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

/** A clean, immediately-closed SSE response — the shape of a normal /agent/run reply the test
 *  never needs to fold (only that it was, or was not, called). */
function closedSSEResponse(): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
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

function typeAndClickRedirect(text: string): void {
  fireEvent.change(screen.getByPlaceholderText('Ask Aura'), { target: { value: text } });
  fireEvent.click(screen.getByRole('button', { name: 'Redirect the current turn' }));
}

describe('ExternalStoreChat — D-10 composer contract (component level)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('a submit while a run is live POSTs the steer route and NEVER a second /agent/run', async () => {
    let steerInit: RequestInit | undefined;
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
      if (url === '/agent/run') return Promise.resolve(openRunStream('run-7'));
      if (url === '/agent/runs/run-7/steer') {
        steerInit = init;
        return Promise.resolve(new Response(null, { status: 202 }));
      }
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first message to open the run');
    await screen.findByRole('button', { name: 'Redirect the current turn' });

    typeAndClickRedirect('check the invoice first');

    await waitFor(() => {
      expect(steerInit).toBeDefined();
    });
    expect(steerInit?.method).toBe('POST');
    expect(new Headers(steerInit?.headers).get('Idempotency-Key')).toBeTruthy();
    // The steer submit issued NO second /agent/run — only the ORIGINAL send that opened the run did.
    expect(fetchMock.mock.calls.filter(([u]) => u === '/agent/run')).toHaveLength(1);
    expect(screen.getByText('check the invoice first')).toBeTruthy(); // optimistic append
  });

  it('a submit with NO live run takes the unchanged /agent/run path (Step-1 measurement)', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
      if (url === '/agent/run') return Promise.resolve(closedSSEResponse());
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('no live run yet');

    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([u]) => u === '/agent/run')).toBe(true);
    });
    expect(fetchMock.mock.calls.some(([u]) => u.includes('/steer'))).toBe(false);
    // No live run ⇒ no dedicated redirect control — Composer's Send stays the composer's Send.
    expect(screen.queryByRole('button', { name: 'Redirect the current turn' })).toBeNull();
  });

  it('rolls back the optimistic steer message on a 400 refusal and shows the refusal text', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
      if (url === '/agent/run') return Promise.resolve(openRunStream('run-7'));
      if (url === '/agent/runs/run-7/steer') {
        return Promise.resolve(new Response("that message can't be redirected", { status: 400 }));
      }
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first message to open the run');
    await screen.findByRole('button', { name: 'Redirect the current turn' });

    typeAndClickRedirect('a refused redirect');

    await waitFor(() => {
      expect(
        screen.getByText("That message couldn't be redirected — try a shorter one."),
      ).toBeTruthy();
    });
    expect(screen.queryByText('a refused redirect')).toBeNull(); // the optimistic append was rolled back
  });

  it('rolls back the optimistic steer message on a 429 refusal and shows the refusal text', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
      if (url === '/agent/run') return Promise.resolve(openRunStream('run-7'));
      if (url === '/agent/runs/run-7/steer') {
        return Promise.resolve(new Response('queue full', { status: 429 }));
      }
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first message to open the run');
    await screen.findByRole('button', { name: 'Redirect the current turn' });

    typeAndClickRedirect('another refused redirect');

    await waitFor(() => {
      expect(
        screen.getByText('Aura already has a redirect queued. Wait a moment and try again.'),
      ).toBeTruthy();
    });
    expect(screen.queryByText('another refused redirect')).toBeNull();
  });
});

describe('useSteerSend', () => {
  function fixture(initialRunId: string | null = 'run-7') {
    const messages: unknown[] = [];
    const setMessages = vi.fn((update: unknown) => {
      const fn = update as (prev: unknown[]) => unknown[];
      messages.splice(0, messages.length, ...(typeof fn === 'function' ? fn(messages) : fn));
    });
    const activeRunIdRef = { current: initialRunId };
    const rendered = renderHook(() =>
      useSteerSend({
        threadId: 'conv-1',
        liveRunId: undefined,
        activeRunIdRef,
        isRunning: initialRunId !== null,
        setMessages,
      }),
    );
    return { rendered, messages, setMessages, activeRunIdRef };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('available is false with no resolvable run id', () => {
    const { rendered } = fixture(null);
    expect(rendered.result.current.available).toBe(false);
  });

  it('trySend returns false (not handled) with no resolvable run id', async () => {
    const { rendered } = fixture(null);
    await expect(rendered.result.current.trySend('hi')).resolves.toBe(false);
  });

  it('onFrame renders one notice when this tab both sent and observes its own echo (dedup by pending text, then by id)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: 202 }))),
    );
    const { rendered } = fixture('run-7');

    await rendered.result.current.trySend('check the invoice first');
    rendered.rerender();
    expect(rendered.result.current.notice?.kind).toBe('redirected');
    const firstNoticeId = rendered.result.current.notice?.id;

    rendered.result.current.onFrame({
      conversation_id: 'conv-1',
      round: 1,
      steers: [
        {
          id: 'steer-1',
          source: 'cockpit',
          text: 'check the invoice first',
          delivery: 'tool_result_append',
        },
      ],
    });
    rendered.rerender();
    // The confirmation did not spawn a SECOND notice — same id as the one shown from send.
    expect(rendered.result.current.notice?.id).toBe(firstNoticeId);
  });

  it('onFrame renders a notice for a steer this tab only observed (never sent)', () => {
    const { rendered } = fixture('run-7');

    rendered.result.current.onFrame({
      conversation_id: 'conv-1',
      round: 1,
      steers: [
        {
          id: 'steer-2',
          source: 'cockpit',
          text: 'from another tab',
          delivery: 'tool_result_append',
        },
      ],
    });
    rendered.rerender();
    expect(rendered.result.current.notice).toEqual({ id: 'steer-2', kind: 'redirected' });
  });

  it('onFrame maps auto_delivery_next_turn to the autoDelivered notice kind', () => {
    const { rendered } = fixture('run-7');

    rendered.result.current.onFrame({
      conversation_id: 'conv-1',
      round: 1,
      steers: [
        { id: 'steer-3', source: 'cockpit', text: 'leftover', delivery: 'auto_delivery_next_turn' },
      ],
    });
    rendered.rerender();
    expect(rendered.result.current.notice).toEqual({ id: 'steer-3', kind: 'autoDelivered' });
  });

  it('onFrame ignores a frame for a different conversation', () => {
    const { rendered } = fixture('run-7');

    rendered.result.current.onFrame({
      conversation_id: 'conv-OTHER',
      round: 1,
      steers: [{ id: 'steer-4', source: 'cockpit', text: 'hi', delivery: 'tool_result_append' }],
    });
    rendered.rerender();
    expect(rendered.result.current.notice).toBeUndefined();
  });

  it('a duplicate frame observation (same id twice) does not re-render a second notice', () => {
    const { rendered } = fixture('run-7');
    const frame = {
      conversation_id: 'conv-1',
      round: 1,
      steers: [{ id: 'steer-5', source: 'cockpit', text: 'hi', delivery: 'tool_result_append' }],
    };
    rendered.result.current.onFrame(frame);
    rendered.rerender();
    rendered.result.current.dismissNotice();
    rendered.rerender();
    rendered.result.current.onFrame(frame); // the SAME id, observed a second time
    rendered.rerender();
    expect(rendered.result.current.notice).toBeUndefined(); // seenIds suppressed the re-add
  });
});
