import type { ReactElement } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AssistantRuntime } from '@assistant-ui/react';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import { WorkerWatchContext, type WorkerWatchController } from '../workers/workerWatchControls';
import type { CoordinatorRunStatus } from '../workers/workerStream';
import {
  isHistoryURL,
  jsonResponse,
  messagesSnapshotResponse,
  sendPrompt,
  sseResponse,
  withTurnSeqs,
} from './chatTestHarness';

// Acceptance 6 and 7 of spec 2026-10-27: the cockpit imports the conversation tree, moves
// between branches without asking the server, and continues the branch on screen.

const captured = vi.hoisted(() => ({ runtime: undefined as AssistantRuntime | undefined }));

vi.mock('@assistant-ui/react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@assistant-ui/react')>();
  return {
    ...actual,
    useExternalStoreRuntime: ((store) => {
      const runtime = actual.useExternalStoreRuntime(store);
      captured.runtime = runtime;
      return runtime;
    }) satisfies typeof actual.useExternalStoreRuntime,
  };
});

afterEach(() => {
  vi.unstubAllGlobals();
  captured.runtime = undefined;
});

const twoBranches = [
  { id: 'msg-1', parentId: null, role: 'user', content: 'first question' },
  { id: 'msg-2', parentId: 'msg-1', role: 'assistant', content: 'first answer' },
  { id: 'msg-3', parentId: null, role: 'user', content: 'question rewritten' },
  { id: 'msg-4', parentId: 'msg-3', role: 'assistant', content: 'answer to the rewrite' },
];

function answer(text: string, headSeq: number, leafSeq: number) {
  return withTurnSeqs(
    [
      { type: 'RUN_STARTED', threadId: 'conv-1', runId: `run-${text}` },
      { type: 'TEXT_MESSAGE_START', messageId: `m-${text}` },
      { type: 'TEXT_MESSAGE_CONTENT', messageId: `m-${text}`, delta: text },
      { type: 'TEXT_MESSAGE_END', messageId: `m-${text}` },
      {
        type: 'RUN_FINISHED',
        threadId: 'conv-1',
        runId: `run-${text}`,
        outcome: { type: 'success' },
      },
    ],
    headSeq,
    leafSeq,
  );
}

interface Posted {
  readonly url: string;
  readonly body: Record<string, unknown>;
}

function stubServer(history: Record<string, readonly Record<string, unknown>[]>) {
  const posted: Posted[] = [];
  const historyReads: string[] = [];
  const replies: (readonly Record<string, unknown>[])[] = [];
  /** Each history read waits for the next gate, when one is queued. */
  const gates: Promise<void>[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (isHistoryURL(url)) {
        const thread = /^\/threads\/([^/]+)\/messages/.exec(url)?.[1] ?? '';
        historyReads.push(thread);
        const gate = gates.shift() ?? Promise.resolve();
        return gate.then(() => messagesSnapshotResponse(history[thread] ?? []));
      }
      if (init?.method === 'POST' && (url === '/agent/run' || url.includes('/edit'))) {
        const raw = typeof init.body === 'string' ? init.body : '{}';
        posted.push({ url, body: JSON.parse(raw) as Record<string, unknown> });
        return Promise.resolve(sseResponse(replies.shift() ?? answer('fallback', 9, 10)));
      }
      return Promise.resolve(jsonResponse([]));
    }),
  );
  return { posted, historyReads, replies, gates };
}

function watching(coordinator?: CoordinatorRunStatus): WorkerWatchController {
  return {
    workers: [],
    statuses: new Map(),
    ...(coordinator !== undefined ? { coordinator } : {}),
    registerWorkers: () => () => undefined,
    watchWorker: () => undefined,
    viewReport: () => undefined,
  };
}

function watchedChat(controller: WorkerWatchController) {
  return (
    <WorkerWatchContext.Provider value={controller}>
      <ExternalStoreChat threadId="conv-1" />
    </WorkerWatchContext.Provider>
  );
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rendered = render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
  return (next: ReactElement) => {
    rendered.rerender(<QueryClientProvider client={client}>{next}</QueryClientProvider>);
  };
}

function readout(text: string) {
  return screen.getByText(
    (_, element) => element?.tagName === 'SPAN' && element.textContent === text,
  );
}

function auraOf(post: Posted | undefined): Record<string, unknown> {
  return (post?.body.aura as Record<string, unknown> | undefined) ?? {};
}

describe('ExternalStoreChat conversation branches', () => {
  it('opens on the branch written last and reads its place among the others', async () => {
    stubServer({ 'conv-1': twoBranches });
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    await screen.findByText('answer to the rewrite');
    expect(screen.queryByText('first answer')).toBeNull();
    expect(readout('2 / 2')).toBeTruthy();
  });

  it('shows the other branch on Previous without asking the server', async () => {
    const server = stubServer({ 'conv-1': twoBranches });
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('answer to the rewrite');
    const reads = server.historyReads.length;

    fireEvent.click(screen.getByRole('button', { name: 'Previous branch' }));

    await screen.findByText('first answer');
    expect(screen.queryByText('answer to the rewrite')).toBeNull();
    expect(readout('1 / 2')).toBeTruthy();
    expect(server.historyReads).toHaveLength(reads);
    expect(server.posted).toEqual([]);
  });

  it('continues the branch on screen: a send after Previous names its last turn', async () => {
    const server = stubServer({ 'conv-1': twoBranches });
    server.replies.push(answer('second answer', 5, 6));
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('answer to the rewrite');
    fireEvent.click(screen.getByRole('button', { name: 'Previous branch' }));
    await screen.findByText('first answer');

    sendPrompt('second question');

    await screen.findByText('second answer');
    expect(auraOf(server.posted[0]).parent_seq).toBe(2);
  });

  // The seqs the run reports land on its messages: what was a guess from the visible position
  // (index + 1) named the wrong turns as soon as the thread had another branch.
  it('addresses the live turns by the seqs their run reported', async () => {
    const server = stubServer({ 'conv-1': twoBranches });
    server.replies.push(answer('reply', 5, 6), answer('reply edited', 7, 8));
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('answer to the rewrite');

    sendPrompt('next question');
    await screen.findByText('reply');
    expect(auraOf(server.posted[0]).parent_seq).toBe(4);

    const edits = screen.getAllByRole('button', { name: 'Edit' });
    const lastEdit = edits[edits.length - 1];
    if (lastEdit === undefined) throw new Error('expected an Edit action');
    fireEvent.click(lastEdit);
    fireEvent.input(await screen.findByRole('textbox', { name: 'Edit message' }), {
      target: { value: 'next question, edited' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save and re-run' }));
    await screen.findByText('reply edited');
    expect(server.posted[1]?.body).toMatchObject({ diverge_seq: 5, role: 'user' });

    const regenerates = screen.getAllByRole('button', { name: 'Regenerate' });
    const lastRegenerate = regenerates[regenerates.length - 1];
    if (lastRegenerate === undefined) throw new Error('expected a Regenerate action');
    fireEvent.click(lastRegenerate);
    await waitFor(() => {
      expect(server.posted).toHaveLength(3);
    });
    expect(server.posted[2]?.body).toMatchObject({ diverge_seq: 8, role: 'assistant' });
  });

  // A run that ends re-reads the history once the coordinator reports it finished, and puts
  // the branch written last on screen. A switch made while that read is in flight is the
  // operator's, and the late read must not take the thread back.
  it('keeps the branch switched to while the read after a run is in flight', async () => {
    const server = stubServer({ 'conv-1': twoBranches });
    const rerender = renderChat(watchedChat(watching()));
    await screen.findByText('answer to the rewrite');
    let release: () => void = () => undefined;
    server.gates.push(
      new Promise((done) => {
        release = done;
      }),
    );
    rerender(watchedChat(watching({ run_id: 'run-1', status: 'finished' })));
    await waitFor(() => {
      expect(server.historyReads).toHaveLength(2);
    });

    fireEvent.click(screen.getByRole('button', { name: 'Previous branch' }));
    await screen.findByText('first answer');
    await act(async () => {
      release();
      await new Promise((settled) => setTimeout(settled, 50));
    });

    expect(screen.getByText('first answer')).toBeTruthy();
    expect(screen.queryByText('answer to the rewrite')).toBeNull();
  });

  it('sends no parent_seq when the thread has no turn yet', async () => {
    const server = stubServer({ 'conv-1': [] });
    server.replies.push(answer('hello', 1, 2));
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await waitFor(() => {
      expect(server.historyReads).toEqual(['conv-1']);
    });

    sendPrompt('hi');

    await screen.findByText('hello');
    expect(auraOf(server.posted[0])).not.toHaveProperty('parent_seq');
  });

  it("keeps none of thread A's branches in the runtime after a switch to B", async () => {
    stubServer({
      'conv-a': twoBranches,
      'conv-b': [{ id: 'msg-1', parentId: null, role: 'user', content: 'thread B' }],
    });
    const rerender = renderChat(<ExternalStoreChat threadId="conv-a" />);
    await screen.findByText('answer to the rewrite');
    expect(captured.runtime?.thread.export().messages).toHaveLength(4);

    rerender(<ExternalStoreChat threadId="conv-b" />);

    await screen.findByText('thread B');
    const kept = captured.runtime?.thread.export().messages.map(({ message }) => message.content);
    expect(kept).toEqual([[{ type: 'text', text: 'thread B' }]]);
  });
});
