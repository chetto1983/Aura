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

/** A run that starts and finishes at once. */
function finishedRunStream(runId: string): Response {
  const frame = (type: string, id: string) =>
    `event: ${type}\nid: ${id}\ndata: ${JSON.stringify({ type, threadId: 'conv-1', runId })}\n\n`;
  return new Response(frame('RUN_STARTED', '1') + frame('RUN_FINISHED', '2'), {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
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

/** The text as a message on the page, not as the composer's own value. */
function threadText(text: string): HTMLElement | null {
  return screen.queryByText(text, { ignore: 'script, style, textarea' });
}

function composerValue(): string {
  return screen.getByPlaceholderText<HTMLTextAreaElement>('Ask Aura').value;
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

  it('rolls back the optimistic steer message on a 400 refusal, shows the refusal text and returns the text to the composer', async () => {
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

    // Both halves inside the same waitFor: the refusal text and the rollback are two effects,
    // and waiting for only the first sampled the second at an arbitrary tick — CI run 1785 saw
    // the optimistic message still on screen when the refusal had already rendered. This waits
    // for the settled state instead, which is what the test always meant to assert.
    await waitFor(() => {
      expect(
        screen.getByText("That message couldn't be redirected — try a shorter one."),
      ).toBeTruthy();
      expect(threadText('a refused redirect')).toBeNull();
      expect(composerValue()).toBe('a refused redirect');
    });
  });

  it('rolls back the optimistic steer message on a 429 refusal, shows the refusal text and returns the text to the composer', async () => {
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
      expect(threadText('another refused redirect')).toBeNull();
      expect(composerValue()).toBe('another refused redirect');
    });
  });

  // Task #30: the server answers 410 to a steer that reaches a run after it ended. The run stream
  // here never closes, so the text goes out while this tab still shows the old run as running.
  it('sends a redirect that reached its run after the run ended as the next turn', async () => {
    const runBodies: string[] = [];
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
      if (url === '/agent/run') {
        runBodies.push(typeof init?.body === 'string' ? init.body : '');
        return Promise.resolve(
          runBodies.length === 1 ? openRunStream('run-7') : finishedRunStream('run-8'),
        );
      }
      if (url === '/agent/runs/run-7/steer') {
        return Promise.resolve(
          new Response('run has ended: message was not queued; send it as a normal turn', {
            status: 410,
          }),
        );
      }
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first message to open the run');
    await screen.findByRole('button', { name: 'Redirect the current turn' });

    typeAndClickRedirect('too late to redirect');

    await waitFor(() => {
      expect(runBodies).toHaveLength(2);
    });
    expect(runBodies[1]).toContain('too late to redirect');
    expect(fetchMock.mock.calls.filter(([u]) => u.endsWith('/steer'))).toHaveLength(1);
    expect(
      await screen.findByText('Delivered as a new message once the previous turn ended.'),
    ).toBeTruthy();
    expect(screen.queryByText(/The turn already finished/)).toBeNull();
    expect(threadText('too late to redirect')).toBeTruthy();
  });

  // The run's POST is still in flight, so the redirect control shows before RUN_STARTED has
  // given this tab an id. A new turn now would collide with the run and be overwritten.
  it('gives a redirect sent before the run has an id back to the composer', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
      if (url === '/agent/run') return new Promise<Response>(() => undefined);
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first message, still being posted');
    await screen.findByRole('button', { name: 'Redirect the current turn' });

    typeAndClickRedirect('too early to redirect');

    await waitFor(() => {
      expect(composerValue()).toBe('too early to redirect');
    });
    expect(
      screen.getByText("Aura can't take a redirect yet — try again in a moment."),
    ).toBeTruthy();
    expect(threadText('too early to redirect')).toBeNull();
    expect(fetchMock.mock.calls.filter(([u]) => u === '/agent/run')).toHaveLength(1);
    expect(fetchMock.mock.calls.some(([u]) => u.endsWith('/steer'))).toBe(false);
  });

  it('keeps what was typed while a refused redirect was in flight, after the returned text', async () => {
    let refuse: (response: Response) => void = () => undefined;
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse());
      if (url === '/agent/run') return Promise.resolve(openRunStream('run-7'));
      if (url === '/agent/runs/run-7/steer') {
        return new Promise<Response>((resolve) => {
          refuse = resolve;
        });
      }
      return Promise.resolve(jsonResponse([]));
    });
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first message to open the run');
    await screen.findByRole('button', { name: 'Redirect the current turn' });

    typeAndClickRedirect('a refused redirect');
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([u]) => u.endsWith('/steer'))).toBe(true);
    });
    fireEvent.change(screen.getByPlaceholderText('Ask Aura'), {
      target: { value: 'and this too' },
    });
    refuse(new Response('too long', { status: 400 }));

    await waitFor(() => {
      expect(composerValue()).toBe('a refused redirect\nand this too');
    });
  });
});

describe('useSteerSend', () => {
  function fixture(
    initialRunId: string | null = 'run-7',
    liveRunId?: string,
    running = initialRunId !== null,
  ) {
    const messages: unknown[] = [{ id: 'earlier' }];
    const setMessages = vi.fn((update: unknown) => {
      const fn = update as (prev: unknown[]) => unknown[];
      messages.splice(0, messages.length, ...(typeof fn === 'function' ? fn(messages) : fn));
    });
    const activeRunIdRef = { current: initialRunId };
    const isRunningRef = { current: running };
    const rendered = renderHook(() =>
      useSteerSend({ threadId: 'conv-1', liveRunId, activeRunIdRef, isRunningRef, setMessages }),
    );
    return { rendered, messages, setMessages, activeRunIdRef };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('trySend returns false (not handled) with no live run', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const { rendered } = fixture(null);
    await expect(rendered.result.current.trySend('hi')).resolves.toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('a run this tab started but has no id for yet takes no steer and gives the text back', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const { rendered, messages } = fixture(null, undefined, true);

    await expect(rendered.result.current.trySend('too early')).resolves.toBe(true);
    rendered.rerender();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(messages).toEqual([{ id: 'earlier' }]);
    expect(rendered.result.current.refusal).toMatchObject({
      message: "Aura can't take a redirect yet — try again in a moment.",
      draft: 'too early',
    });
  });

  it.each([
    ['the run this tab drives', 'run-7', undefined],
    ['a cached live_run_id', null, 'run-old'],
  ])(
    'a 410 from %s leaves the text to the caller as a turn, and that run is never steered again',
    async (_, activeRunId, liveRunId) => {
      const fetchMock = vi.fn(() =>
        Promise.resolve(new Response('run has ended', { status: 410 })),
      );
      vi.stubGlobal('fetch', fetchMock);
      const { rendered, messages } = fixture(activeRunId, liveRunId);

      await expect(rendered.result.current.trySend('too late')).resolves.toBe(false);
      rendered.rerender();
      expect(messages).toEqual([{ id: 'earlier' }]);
      expect(rendered.result.current.notice?.kind).toBe('autoDelivered');
      expect(rendered.result.current.refusal).toBeUndefined();

      await expect(rendered.result.current.trySend('again')).resolves.toBe(false);
      expect(fetchMock).toHaveBeenCalledTimes(1);
    },
  );

  it.each([
    [429, 'Aura already has a redirect queued. Wait a moment and try again.'],
    [500, "Couldn't redirect the turn. Try again."],
  ])('a %i refusal is handled here and carries the text back', async (status, message) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('refused', { status }))),
    );
    const { rendered, messages } = fixture('run-7');

    await expect(rendered.result.current.trySend('later please')).resolves.toBe(true);
    rendered.rerender();
    expect(messages).toEqual([{ id: 'earlier' }]);
    expect(rendered.result.current.refusal).toMatchObject({ message, draft: 'later please' });
  });

  it('an empty run id is no run to steer', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const { rendered } = fixture('', '');
    await expect(rendered.result.current.trySend('hi')).resolves.toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('a new steer clears the refusal of the last one, and each notice has its own id', async () => {
    const statuses = [429, 202, 202, 410, 410];
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: statuses.shift() ?? 500 }))),
    );
    const { rendered, activeRunIdRef } = fixture('run-7');

    await rendered.result.current.trySend('refused');
    rendered.rerender();
    expect(rendered.result.current.refusal).toBeDefined();

    const notices: (string | undefined)[] = [];
    for (const [runId, text] of [
      ['run-7', 'first'],
      ['run-7', 'second'],
      ['run-8', 'late'],
      ['run-9', 'later'],
    ]) {
      activeRunIdRef.current = runId ?? null;
      await rendered.result.current.trySend(text ?? '');
      rendered.rerender();
      expect(rendered.result.current.refusal).toBeUndefined();
      notices.push(rendered.result.current.notice?.id);
    }
    expect(new Set(notices).size).toBe(4);
  });

  it('a steer another channel sent with the same text still shows its notice', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => undefined)),
    );
    const { rendered } = fixture('run-7');
    void rendered.result.current.trySend('same words');

    rendered.result.current.onFrame({
      conversation_id: 'conv-1',
      round: 1,
      steers: [
        { id: 'tg-1', source: 'telegram', text: 'same words', delivery: 'tool_result_append' },
      ],
    });
    rendered.rerender();
    expect(rendered.result.current.notice).toEqual({ id: 'tg-1', kind: 'redirected' });
  });

  it('onFrame follows the thread the hook is rendered for', () => {
    const rendered = renderHook(
      ({ threadId }: { threadId: string }) =>
        useSteerSend({
          threadId,
          liveRunId: undefined,
          activeRunIdRef: { current: 'run-7' },
          isRunningRef: { current: true },
          setMessages: vi.fn(),
        }),
      { initialProps: { threadId: 'conv-1' } },
    );
    rendered.rerender({ threadId: 'conv-2' });

    rendered.result.current.onFrame({
      conversation_id: 'conv-2',
      round: 1,
      steers: [{ id: 'steer-9', source: 'cockpit', text: 'hi', delivery: 'tool_result_append' }],
    });
    rendered.rerender({ threadId: 'conv-2' });
    expect(rendered.result.current.notice).toEqual({ id: 'steer-9', kind: 'redirected' });
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

  it.each(['swarm', 'shell', 'media'])(
    'onFrame shows no notice for Aura’s own %s fact, which the operator never sent',
    (source) => {
      const { rendered } = fixture('run-7');

      rendered.result.current.onFrame({
        conversation_id: 'conv-1',
        round: 1,
        steers: [
          {
            id: `runtime-${source}`,
            source,
            text: 'job finished',
            delivery: 'user_message_fallback',
          },
        ],
      });
      rendered.rerender();
      expect(rendered.result.current.notice).toBeUndefined();
    },
  );

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
