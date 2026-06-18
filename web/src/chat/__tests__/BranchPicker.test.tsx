import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from '../ExternalStoreChat';

// Build a single SSE wire body from raw AG-UI frame objects (the same shapes the Go
// translator emits + the branch re-run streams).
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

function messagesSnapshotResponse(messages: readonly Record<string, unknown>[] = []): Response {
  return new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function isHistoryURL(url: unknown): boolean {
  return typeof url === 'string' && url.startsWith('/threads/');
}

const turnFrames = (msgId: string, text: string) => [
  { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-1' },
  { type: 'TEXT_MESSAGE_START', messageId: msgId },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: msgId, delta: text },
  { type: 'TEXT_MESSAGE_END', messageId: msgId },
  { type: 'RUN_FINISHED', threadId: 'conv-1', runId: 'run-1', outcome: { type: 'success' } },
];

function sendPrompt(text: string): void {
  const input = screen.getByPlaceholderText('Ask Aura');
  fireEvent.change(input, { target: { value: text } });
  fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

interface FetchCall {
  readonly url: string;
  readonly init: RequestInit | undefined;
}

// editCalls extracts the /edit branch re-run fetch calls (url + init) from the mock,
// typed so the tuple-index strictness (noUncheckedIndexedAccess) is satisfied.
function editCalls(mock: { mock: { calls: unknown[][] } }): FetchCall[] {
  return mock.mock.calls
    .map((c) => ({ url: String(c[0]), init: c[1] as RequestInit | undefined }))
    .filter((c) => c.url.includes('/edit'));
}

function bodyOf(call: FetchCall | undefined): Record<string, unknown> {
  const raw = call?.init?.body;
  return typeof raw === 'string' ? (JSON.parse(raw) as Record<string, unknown>) : {};
}

describe('BranchPicker + edit/reload (CHAT-05 / D-09)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('ships ONLY Copy/Edit/Reload action verbs — no feedback rating group (Phase 26 boundary)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) =>
        Promise.resolve(
          isHistoryURL(url) ? messagesSnapshotResponse() : sseResponse(turnFrames('m1', 'hello')),
        ),
      ),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('hi');
    await waitFor(() => {
      expect(screen.getByText('hello')).toBeTruthy();
    });

    // Copy (assistant + user), Edit (user), Reload (assistant) ARE present.
    expect(screen.getAllByRole('button', { name: 'Copy' }).length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Edit' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Regenerate' })).toBeTruthy();

    // NO feedback rating group (thumbs up/down) — held out for Phase 26. (The regex is
    // anchored so it does not accidentally match "Regenerate" via a "rate" substring.)
    expect(
      screen.queryByRole('button', { name: /thumbs|feedback|^rate$|^like$|dislike/i }),
    ).toBeNull();
  });

  it('Reload regenerates an assistant turn via onReload -> POST /edit (assistant branch)', async () => {
    const fetchMock = vi.fn((url: string) =>
      Promise.resolve(
        isHistoryURL(url)
          ? messagesSnapshotResponse()
          : sseResponse(turnFrames('m1', 'first answer')),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('weather?');
    await waitFor(() => {
      expect(screen.getByText('first answer')).toBeTruthy();
    });

    // Regenerate the assistant turn → POST the branch edit route.
    fetchMock.mockImplementation((url: string) =>
      Promise.resolve(
        isHistoryURL(url)
          ? messagesSnapshotResponse()
          : sseResponse(turnFrames('m2', 'regenerated answer')),
      ),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Regenerate' }));

    await waitFor(() => {
      expect(screen.getByText('regenerated answer')).toBeTruthy();
    });
    // The reload POSTed the branch edit route (assistant regenerate), not /agent/run.
    const editCall = editCalls(fetchMock)[0];
    expect(editCall).toBeDefined();
    expect(bodyOf(editCall).role).toBe('assistant');
  });

  it('Edit a user turn enters edit mode and on submit forks a branch via onEdit -> POST /edit', async () => {
    const fetchMock = vi.fn((url: string) =>
      Promise.resolve(
        isHistoryURL(url)
          ? messagesSnapshotResponse()
          : sseResponse(turnFrames('m1', 'answer one')),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first question');
    await waitFor(() => {
      expect(screen.getByText('answer one')).toBeTruthy();
    });

    // Enter edit mode on the user turn → the message-scoped edit composer appears.
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const editor = await screen.findByRole('textbox', { name: 'Edit message' });
    fireEvent.input(editor, { target: { value: 'edited question' } });

    // Submit the edit (Save and re-run) → onEdit fires → POST /edit with role:user.
    fetchMock.mockImplementation((url: string) =>
      Promise.resolve(
        isHistoryURL(url)
          ? messagesSnapshotResponse()
          : sseResponse(turnFrames('m2', 'answer two')),
      ),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save and re-run' }));

    await waitFor(() => {
      expect(editCalls(fetchMock).length).toBeGreaterThan(0);
    });
    const body = bodyOf(editCalls(fetchMock)[0]);
    expect(body.role).toBe('user');
    expect(body.content).toBe('edited question');

    // After the edit forks a sibling, the BranchPicker appears (n / count) with a11y nav.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Next branch' })).toBeTruthy();
    });
    expect(screen.getByRole('button', { name: 'Previous branch' })).toBeTruthy();
  });

  it('Edit uses the persisted backend seq from a rehydrated no-system conversation', async () => {
    const fetchMock = vi.fn((url: string) =>
      Promise.resolve(
        isHistoryURL(url)
          ? messagesSnapshotResponse([
              { id: 'msg-1', role: 'user', content: 'loaded question' },
              { id: 'msg-2', role: 'assistant', content: 'loaded answer' },
            ])
          : sseResponse(turnFrames('m2', 'edited answer')),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('loaded answer');

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const editor = await screen.findByRole('textbox', { name: 'Edit message' });
    fireEvent.input(editor, { target: { value: 'edited loaded question' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save and re-run' }));

    await waitFor(() => {
      expect(editCalls(fetchMock).length).toBeGreaterThan(0);
    });
    const body = bodyOf(editCalls(fetchMock)[0]);
    expect(body.role).toBe('user');
    expect(body.diverge_seq).toBe(1);
  });

  it('Reload uses the persisted assistant seq from a rehydrated no-system conversation', async () => {
    const fetchMock = vi.fn((url: string) =>
      Promise.resolve(
        isHistoryURL(url)
          ? messagesSnapshotResponse([
              { id: 'msg-1', role: 'user', content: 'loaded question' },
              { id: 'msg-2', role: 'assistant', content: 'loaded answer' },
            ])
          : sseResponse(turnFrames('m2', 'regenerated loaded answer')),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('loaded answer');

    fireEvent.click(screen.getByRole('button', { name: 'Regenerate' }));

    await waitFor(() => {
      expect(editCalls(fetchMock).length).toBeGreaterThan(0);
    });
    const body = bodyOf(editCalls(fetchMock)[0]);
    expect(body.role).toBe('assistant');
    expect(body.diverge_seq).toBe(2);
  });
});
