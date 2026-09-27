import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement, useState, type ReactNode } from 'react';
import { act, renderHook, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../i18n/i18n';
import { useThreadElicitations } from '../questions/useThreadElicitations';
import { useLiveRunAttach, type LiveRunAttachArgs } from './ExternalStoreChat_liveRun';
import { ExternalStoreChat } from './ExternalStoreChat';
import { renderChat, sendPrompt } from './__tests__/chatTestHarness';
import { streamRun, type AguiFrame } from './sseAdapter';
import { elicitationQuestionOf, elicitationSignalValue } from './sseAdapter_elicitation';
import { attachRun } from './sseResume';

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

// The aura.elicitation pump signal, on the driving pump and the reattach pump alike, shaped
// like sseAdapter.onSteer.test.ts. A form is never reduced into a message part.

function sseResponse(frames: readonly AguiFrame[]): Response {
  const enc = new TextEncoder();
  const wire = frames.map((f) => `event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`).join('');
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

const RUN_STARTED = { type: 'RUN_STARTED' } as AguiFrame;
const RUN_FINISHED = { type: 'RUN_FINISHED', outcome: { type: 'success' } } as AguiFrame;

const QUESTION = {
  run_id: 'run-1',
  id: 'q-1',
  server: 'forms',
  tool: 'ask_name',
  message: 'what is your name',
  fields: [
    { name: 'name', kind: 'string', required: true },
    { name: 'tags', kind: 'enum', required: false, multi: true, min_items: 1, max_items: 3 },
  ],
  deadline: '2026-09-25T10:05:00Z',
};

function custom(name: string, value: unknown): AguiFrame {
  return { type: 'CUSTOM', name, value };
}

describe('elicitationSignalValue', () => {
  it('reads a question and its resolution', () => {
    expect(elicitationSignalValue(custom('aura.elicitation', QUESTION))).toEqual({
      kind: 'question',
      question: QUESTION,
    });
    const expired = { id: 'q-1', action: 'cancel', expired: true };
    expect(elicitationSignalValue(custom('aura.elicitation_resolved', expired))).toEqual({
      kind: 'resolved',
      resolved: expired,
    });
  });

  it('reads a refusal, whose fields the server sends as null', () => {
    const refused = { ...QUESTION, fields: null, message: '', refusal: 'unrenderable' };
    expect(elicitationSignalValue(custom('aura.elicitation', refused))).toEqual({
      kind: 'question',
      question: { ...refused, fields: [] },
    });
  });

  it('drops what it cannot trust', () => {
    for (const bad of [
      { ...QUESTION, id: 7 },
      { ...QUESTION, fields: [{ name: 'x', kind: 'object', required: true }] },
      { ...QUESTION, fields: [{ name: 'x', kind: 'string', required: true, format: 'ipv4' }] },
      { ...QUESTION, fields: [{ name: 'x', kind: 'enum', required: true, max_items: '3' }] },
      { ...QUESTION, refusal: 'because' },
      { ...QUESTION, fields: null },
      { ...QUESTION, fields: undefined },
      'not an object',
    ]) {
      expect(elicitationSignalValue(custom('aura.elicitation', bad))).toBeNull();
    }
    const unknownAction = { id: 'q-1', action: 'sure' };
    expect(elicitationSignalValue(custom('aura.elicitation_resolved', unknownAction))).toBeNull();
    expect(elicitationSignalValue(custom('aura.steer', QUESTION))).toBeNull();
    expect(elicitationSignalValue({ type: 'TEXT_MESSAGE_START', messageId: 'm1' })).toBeNull();
  });

  it('reads the same question shape the run lists', () => {
    expect(elicitationQuestionOf(QUESTION)).toEqual(QUESTION);
    expect(elicitationQuestionOf({ ...QUESTION, deadline: 5 })).toBeNull();
  });
});

describe('the pumps fire onElicitation', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const frames = [
    RUN_STARTED,
    custom('aura.elicitation', QUESTION),
    custom('aura.elicitation_resolved', { id: 'q-1', action: 'accept' }),
    RUN_FINISHED,
  ];

  it('on the driving pump', async () => {
    const onElicitation = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(sseResponse(frames))),
    );
    await streamRun({
      threadId: 'conv-1',
      userText: 'ask me',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onElicitation,
    });
    const kinds = onElicitation.mock.calls.map(([signal]) => (signal as { kind: string }).kind);
    expect(kinds).toEqual(['question', 'resolved']);
  });

  it('on the reattach pump, so a reloaded tab gets the form back', async () => {
    const onElicitation = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(sseResponse(frames))),
    );
    await attachRun({
      threadId: 'conv-1',
      runId: 'run-1',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onElicitation,
    });
    expect(onElicitation).toHaveBeenCalledTimes(2);
  });
});

describe('reattach list and replay reconciliation', () => {
  afterEach(() => vi.unstubAllGlobals());

  function harness(replayGone = false) {
    let resolveList: (response: Response) => void = () => undefined;
    const promise = new Promise<Response>((resolve) => {
      resolveList = resolve;
    });
    const list = { promise, resolve: resolveList };
    const stream = new TransformStream<Uint8Array, Uint8Array>();
    const writer = stream.writable.getWriter();
    const controller = new AbortController();
    const updates = vi.fn();
    const finished = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = requestURL(input);
        if (url.endsWith('/elicitations')) return list.promise;
        if (url.includes('/events'))
          return Promise.resolve(
            replayGone
              ? new Response('replay gone', { status: 410 })
              : new Response(stream.readable),
          );
        if (url.endsWith('/messages')) return Promise.resolve(Response.json([]));
        return Promise.resolve(Response.json({ live_run_id: 'run-1' }));
      }),
    );
    const fold: LiveRunAttachArgs['foldAppendedStream'] = async (_id, run) => {
      try {
        await run(controller, updates);
      } finally {
        activeRunIdRef.current = null;
        finished();
      }
    };
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const refs = { current: false };
    const activeRunIdRef = { current: null as string | null };
    const hook = renderHook(
      () => {
        const forms = useThreadElicitations('conv-1', true, 'run-1');
        useLiveRunAttach({
          threadId: 'conv-1',
          liveRunId: 'run-1',
          historyReadiness: { threadId: 'conv-1', status: 'ready' },
          isRunningRef: refs,
          activeRunIdRef,
          foldAppendedStream: fold,
          setMessages: () => undefined,
          onElicitation: forms.onSignal,
        });
        return forms;
      },
      {
        wrapper: ({ children }: { children: ReactNode }) =>
          createElement(QueryClientProvider, { client: queryClient }, children),
      },
    );
    async function emit(...frames: AguiFrame[]) {
      await act(async () => {
        await writer.write(
          new TextEncoder().encode(
            frames.map((frame) => `data: ${JSON.stringify(frame)}\n\n`).join(''),
          ),
        );
      });
    }
    return { ...hook, list, writer, controller, updates, finished, emit };
  }

  it('applies an in-flight resolution after the list and keeps pending forms in arrival order', async () => {
    const h = harness();
    await waitFor(() => {
      expect(h.updates).toHaveBeenCalled();
    });
    await h.emit(
      custom('aura.elicitation', { ...QUESTION, id: 'q-2' }),
      custom('aura.elicitation_resolved', { id: 'q-1', action: 'cancel', expired: true }),
    );
    await act(async () => {
      h.list.resolve(Response.json({ questions: [QUESTION, { ...QUESTION, id: 'q-2' }] }));
      await h.list.promise;
    });
    await waitFor(() => {
      expect(h.result.current.items.map((item) => [item.question.id, item.outcome])).toEqual([
        ['q-1', 'expired'],
        ['q-2', undefined],
      ]);
    });
    await h.emit(RUN_FINISHED);
    await h.writer.close();
    h.unmount();
  });

  it('ignores a late list after the stream was stopped', async () => {
    const h = harness();
    await waitFor(() => {
      expect(h.updates).toHaveBeenCalled();
    });
    h.controller.abort();
    await act(async () => {
      h.list.resolve(Response.json({ questions: [QUESTION] }));
      await h.list.promise;
    });
    expect(h.result.current.items).toEqual([]);
    await h.writer.close();
    h.unmount();
  });

  it('restores a form from a late list after the replay has rotated away', async () => {
    const h = harness(true);
    await waitFor(() => {
      expect(h.finished).toHaveBeenCalled();
    });
    await act(async () => {
      h.list.resolve(Response.json({ questions: [QUESTION] }));
      await h.list.promise;
    });
    expect(h.result.current.items.map((item) => item.question.id)).toEqual(['q-1']);
    h.unmount();
  });
});

it('shows an elicitation on the first turn after creating the conversation', async () => {
  const stream = new TransformStream<Uint8Array, Uint8Array>();
  const writer = stream.writable.getWriter();
  const started = vi.fn();
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      if (requestURL(input) === '/agent/run') {
        started();
        return Promise.resolve(new Response(stream.readable));
      }
      return Promise.resolve(Response.json([]));
    }),
  );
  function FirstSend() {
    const [threadId, setThreadId] = useState('');
    return createElement(ExternalStoreChat, {
      threadId,
      onEnsureThread: () => {
        setThreadId('conv-created');
        return Promise.resolve('conv-created');
      },
    });
  }
  const view = renderChat(createElement(FirstSend));
  try {
    sendPrompt('Ask my name');
    await waitFor(() => {
      expect(started).toHaveBeenCalled();
    });
    await act(async () => {
      const frames = [
        { type: 'RUN_STARTED', runId: 'run-1', threadId: 'conv-created' },
        custom('aura.elicitation', QUESTION),
      ];
      await writer.write(
        new TextEncoder().encode(
          frames.map((frame) => `data: ${JSON.stringify(frame)}\n\n`).join(''),
        ),
      );
    });
    expect(await screen.findByRole('heading', { name: 'name' })).toBeTruthy();
  } finally {
    await act(async () => {
      await writer.close();
    });
    view.unmount();
    vi.unstubAllGlobals();
  }
});
