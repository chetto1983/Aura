import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from '../ExternalStoreChat';
import { AttachmentChip } from '../attachments/AttachmentChip';
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

function messagesSnapshotResponse(messages: readonly Record<string, unknown>[]): Response {
  return new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function isHistoryURL(url: unknown): boolean {
  return typeof url === 'string' && url.startsWith('/threads/');
}

function completedTurn(messageId: string, text: string): readonly Record<string, unknown>[] {
  return [
    { type: 'RUN_STARTED', threadId: 'conv-1', runId: `run-${messageId}` },
    { type: 'TEXT_MESSAGE_START', messageId },
    { type: 'TEXT_MESSAGE_CONTENT', messageId, delta: text },
    { type: 'TEXT_MESSAGE_END', messageId },
    {
      type: 'RUN_FINISHED',
      threadId: 'conv-1',
      runId: `run-${messageId}`,
      outcome: { type: 'success' },
    },
  ];
}

function expectRequiredTouchTarget(element: Element): void {
  const classes = element.getAttribute('class') ?? '';
  expect(element.getAttribute('data-required-touch-target')).not.toBeNull();
  expect(classes).toMatch(/(?:^|\s)(?:min-h-11|h-11)(?:\s|$)/);
  expect(classes).toMatch(/(?:^|\s)(?:min-w-11|w-11)(?:\s|$)/);
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
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(messagesSnapshotResponse([]))),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    expect(screen.getByText('Type a prompt below to start this run.')).toBeTruthy();
    expect(screen.getByPlaceholderText('Ask Aura')).toBeTruthy();
  });

  it('rehydrates persisted messages when an existing conversation opens', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        messagesSnapshotResponse([
          { id: 'msg-2', role: 'user', content: 'persisted prompt' },
          { id: 'msg-3', role: 'assistant', content: 'persisted answer' },
        ]),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    renderChat(<ExternalStoreChat threadId="conv-1" />);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/threads/conv-1/messages',
        expect.objectContaining({ method: 'GET', credentials: 'same-origin' }),
      );
    });
    expect(await screen.findByText('persisted prompt')).toBeTruthy();
    expect(screen.getByText('persisted answer')).toBeTruthy();
  });

  it('keeps assistant content fluid, caps prose alone, and exposes responsive action rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) =>
        Promise.resolve(
          typeof url === 'string' && url.startsWith('/api/assets')
            ? new Response('[]', { status: 200 })
            : messagesSnapshotResponse([
                { id: 'msg-2', role: 'user', content: 'persisted prompt' },
                { id: 'msg-3', role: 'assistant', content: 'persisted answer' },
              ]),
        ),
      ),
    );

    const { container } = renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('persisted answer');

    const assistant = container.querySelector('[data-message-role="assistant"]');
    if (!(assistant instanceof HTMLElement)) throw new Error('expected assistant message root');
    expect(assistant.classList.contains('w-full')).toBe(true);
    expect(assistant.classList.contains('min-w-0')).toBe(true);

    const content = assistant.querySelector('[data-message-content]');
    const prose = assistant.querySelector('[data-message-prose]');
    if (!(content instanceof HTMLElement) || !(prose instanceof HTMLElement)) {
      throw new Error('expected separate assistant content and prose regions');
    }
    expect(content.classList.contains('w-full')).toBe(true);
    expect(content.classList.contains('min-w-0')).toBe(true);
    expect(content.classList.contains('overflow-x-auto')).toBe(true);
    expect(content.classList.contains('max-w-[48rem]')).toBe(false);
    expect(prose.classList.contains('max-w-[48rem]')).toBe(true);
    expect(prose.classList.contains('[overflow-wrap:anywhere]')).toBe(true);
    expect(content.contains(prose)).toBe(true);

    const actionRows = Array.from(container.querySelectorAll('[data-message-actions]'));
    expect(actionRows).toHaveLength(2);
    for (const row of actionRows) {
      expect(row.classList.contains('flex-wrap')).toBe(true);
      expect(row.classList.contains('opacity-0')).toBe(true);
      expect(row.classList.contains('hover:opacity-100')).toBe(true);
      expect(row.classList.contains('focus-within:opacity-100')).toBe(true);
      expect(row.classList.contains('[@media(pointer:coarse)]:opacity-100')).toBe(true);
    }

    expectRequiredTouchTarget(screen.getByRole('button', { name: 'Edit' }));
    expectRequiredTouchTarget(screen.getByRole('button', { name: 'Regenerate' }));
    for (const copy of screen.getAllByRole('button', { name: 'Copy' })) {
      expectRequiredTouchTarget(copy);
    }
  });

  it('streams an assistant answer with reasoning drawer + tool card over /agent/run', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (isHistoryURL(url)) return Promise.resolve(messagesSnapshotResponse([]));
      return Promise.resolve(
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
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const usageSpy = vi.fn();
    const { container } = renderChat(<ExternalStoreChat threadId="conv-1" onUsage={usageSpy} />);
    sendPrompt('weather?');

    // The assistant text renders (markdown).
    await waitFor(() => {
      expect(screen.getByText('It is sunny.')).toBeTruthy();
    });
    const proseBlocks = Array.from(container.querySelectorAll('.text-base.leading-relaxed'));
    expect(proseBlocks.some((el) => el.textContent?.includes('weather?'))).toBe(true);
    expect(proseBlocks.some((el) => el.textContent?.includes('It is sunny.'))).toBe(true);

    // POST went to /agent/run with the AG-UI body.
    expect(fetchMock.mock.calls.some((call) => call[0] === '/agent/run')).toBe(true);

    // Reasoning drawer rendered the CoT (shown by default).
    expect(screen.getByText('let me think')).toBeTruthy();

    // Tool card shows the tool name + done status; expanding reveals the raw blob.
    expect(screen.getByText('web_search')).toBeTruthy();
    expect(
      screen.getByText('web_search').compareDocumentPosition(screen.getByText('It is sunny.')) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Show raw result' }));
    expect(screen.getByText('sunny 25C')).toBeTruthy();

    // Usage flowed to the footer seam (25-04).
    await waitFor(() => {
      expect(usageSpy).toHaveBeenCalled();
    });
    const lastUsage = usageSpy.mock.calls.at(-1)?.[0] as { promptTokens: number } | undefined;
    expect(lastUsage?.promptTokens).toBe(120);
  });

  it('routes a tool turn carrying an aura.display payload through the DisplayRouter (DISP-02)', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (isHistoryURL(url)) return Promise.resolve(messagesSnapshotResponse([]));
      return Promise.resolve(
        sseResponse([
          { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-1' },
          { type: 'TOOL_CALL_START', toolCallId: 'call-1', toolCallName: 'web_search' },
          { type: 'TOOL_CALL_ARGS', toolCallId: 'call-1', delta: '{"q":"meteo"}' },
          { type: 'TOOL_CALL_END', toolCallId: 'call-1' },
          { type: 'TOOL_CALL_RESULT', toolCallId: 'call-1', content: '[1] Forecast' },
          // The backend emits the typed display on completion (D-15 progressive swap).
          {
            type: 'CUSTOM',
            name: 'aura.display',
            value: {
              type: 'web_result',
              tool_call_id: 'call-1',
              title: 'meteo',
              web_results: [{ title: 'Forecast', url: 'https://example.com', snippet: 'sunny' }],
            },
          },
          { type: 'TEXT_MESSAGE_START', messageId: 'msg-1' },
          { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-1', delta: 'It is sunny.' },
          { type: 'TEXT_MESSAGE_END', messageId: 'msg-1' },
          {
            type: 'RUN_FINISHED',
            threadId: 'conv-1',
            runId: 'run-1',
            outcome: { type: 'success' },
          },
        ]),
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container } = renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('weather?');

    // 26-05 wired the web_result case: the aura.display payload now routes through
    // the DisplayRouter to the rich WebResultDisplay (not the raw fallback). The
    // typed card renders the result snippet + the "Web results" label alongside the
    // assistant answer (the typed display upgrades the tool turn in place, D-15).
    await waitFor(() => {
      expect(screen.getByText('It is sunny.')).toBeTruthy();
    });
    expect(screen.getByText('sunny')).toBeTruthy();
    expect(screen.getByText('Web results')).toBeTruthy();
    expect(screen.getByText('Forecast')).toBeTruthy();
    expect(screen.getByText('Web results').closest('[data-message-prose]')).toBeNull();
    expect(screen.getByText('Web results').closest('[data-message-content]')).not.toBeNull();
    expect(container.querySelectorAll('[data-message-prose]')).toHaveLength(1);
  });

  it('keeps both branch arrows at the required touch target after a real edit fork', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/api/assets'))
        return Promise.resolve(new Response('[]', { status: 200 }));
      if (isHistoryURL(url)) return Promise.resolve(messagesSnapshotResponse([]));
      const edited = url.includes('/edit');
      return Promise.resolve(
        sseResponse(
          completedTurn(
            edited ? 'msg-edited' : 'msg-first',
            edited ? 'edited answer' : 'first answer',
          ),
        ),
      );
    });
    vi.stubGlobal('fetch', fetchMock);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first question');
    await screen.findByText('first answer');

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.input(await screen.findByRole('textbox', { name: 'Edit message' }), {
      target: { value: 'edited question' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save and re-run' }));

    expectRequiredTouchTarget(await screen.findByRole('button', { name: 'Previous branch' }));
    expectRequiredTouchTarget(screen.getByRole('button', { name: 'Next branch' }));
  });

  it('contains long attachment filenames without allowing remove or download actions to shrink', async () => {
    const longName = `${'quarterly-evidence-'.repeat(12)}.pdf`;
    const onRemove = vi.fn();
    const chipRender = render(
      <AttachmentChip
        item={{
          localId: 'upload-1',
          file: new File(['document'], longName, { type: 'application/pdf' }),
          progress: 1,
          status: 'ready',
        }}
        onRemove={onRemove}
      />,
    );
    const remove = screen.getByRole('button', { name: `Remove ${longName}` });
    const chip = remove.parentElement;
    if (!(chip instanceof HTMLElement)) throw new Error('expected attachment chip');
    expect(chip.classList.contains('min-w-0')).toBe(true);
    expect(chip.classList.contains('max-w-full')).toBe(true);
    expect(remove.classList.contains('shrink-0')).toBe(true);
    expectRequiredTouchTarget(remove);
    chipRender.unmount();

    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (url === '/threads/conv-1/messages') {
          return Promise.resolve(
            messagesSnapshotResponse([
              { id: 'msg-1', role: 'user', content: 'make a document' },
              { id: 'msg-2', role: 'assistant', content: 'document ready' },
            ]),
          );
        }
        if (url === '/api/assets?thread_id=conv-1') {
          return Promise.resolve(
            new Response(
              JSON.stringify([
                {
                  id: 'asset-long',
                  source_kind: 'agent',
                  status: 'complete',
                  modality: 'document',
                  file_name: longName,
                  mime_type: 'application/pdf',
                  declared_size_bytes: 8,
                  size_bytes: 8,
                },
              ]),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        return Promise.reject(new Error(`unexpected fetch: ${String(url)}`));
      }),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    const download = await screen.findByRole('link', { name: `Download ${longName}` });
    const downloadOwner = download.parentElement;
    if (!(downloadOwner instanceof HTMLElement)) throw new Error('expected download owner');
    expect(downloadOwner.classList.contains('min-w-0')).toBe(true);
    expect(download.classList.contains('max-w-full')).toBe(true);
    expectRequiredTouchTarget(download);
    const filename = screen.getByText(longName);
    expect(filename.classList.contains('min-w-0')).toBe(true);
    expect(filename.classList.contains('[overflow-wrap:anywhere]')).toBe(true);
  });

  it('sizes persisted attachment Retry, Promote, and Remove actions for touch', async () => {
    const baseAsset = {
      modality: 'document',
      mime_type: 'application/pdf',
      declared_size_bytes: 8,
      size_bytes: 8,
    };
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (url === '/threads/conv-1/messages') {
          return Promise.resolve(
            messagesSnapshotResponse([
              { id: 'msg-1', role: 'user', content: 'failed document' },
              { id: 'msg-2', role: 'assistant', content: 'retry available' },
              { id: 'msg-3', role: 'user', content: 'ready document' },
              { id: 'msg-4', role: 'assistant', content: 'promotion available' },
            ]),
          );
        }
        if (url === '/api/assets?thread_id=conv-1') {
          return Promise.resolve(
            new Response(
              JSON.stringify([
                { ...baseAsset, id: 'asset-failed', status: 'failed', file_name: 'broken.pdf' },
                { ...baseAsset, id: 'asset-ready', status: 'complete', file_name: 'ready.pdf' },
              ]),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        return Promise.reject(new Error(`unexpected fetch: ${String(url)}`));
      }),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    await screen.findByText('broken.pdf');
    const actions = [
      screen.getByRole('button', { name: 'Retry' }),
      screen.getByRole('button', { name: 'Promote' }),
      ...screen.getAllByRole('button', { name: /^Remove (?:broken|ready)\.pdf$/ }),
    ];
    expect(actions).toHaveLength(4);
    actions.forEach(expectRequiredTouchTarget);
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
    const fetchMock = vi.fn((url: string, init: RequestInit) => {
      if (isHistoryURL(url)) return Promise.resolve(messagesSnapshotResponse([]));
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
    const runCall = fetchMock.mock.calls.find((call) => call[0] === '/agent/run') as
      | [string, RequestInit]
      | undefined;
    if (runCall === undefined) throw new Error('expected /agent/run fetch call');
    const init = runCall[1];
    expect(init.signal).toBeInstanceOf(AbortSignal);
  });

  // AC-5: once a turn completes (RUN_FINISHED → onNew finally), the conversation
  // React-Query read is invalidated so the persisted Session totals refresh.
  it('invalidates the conversation query key when a turn completes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) =>
        Promise.resolve(
          isHistoryURL(url)
            ? messagesSnapshotResponse([])
            : sseResponse([
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
      vi.fn((url: string) =>
        Promise.resolve(
          isHistoryURL(url)
            ? messagesSnapshotResponse([])
            : sseResponse([
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
