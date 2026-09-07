import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
import type { SetStateAction } from 'react';
import { fetchThreadMessages } from '../sseAdapter';
import { preserveMessageIDs, useWorkerReportRefresh } from './useWorkerReportRefresh';

let reported = true;
vi.mock('./workerWatchControls', () => ({
  useWatchWorker: () => ({ statuses: new Map([['w1', { child_id: 'w1', reported }]]) }),
}));
vi.mock('../sseAdapter', () => ({ fetchThreadMessages: vi.fn() }));
const messages: ThreadMessageLike[] = [
  { role: 'assistant', content: [{ type: 'text', text: 'Worker result: 99' }] },
];

describe('worker report refresh', () => {
  beforeEach(() => {
    reported = true;
    vi.mocked(fetchThreadMessages).mockReset().mockResolvedValue(messages);
  });
  it('refreshes after report persistence, not merely model completion', async () => {
    reported = false;
    const setMessages = vi.fn<(value: SetStateAction<ThreadMessageLike[]>) => void>();
    const options = {
      threadId: 'conv',
      isRunning: false,
      historyRequestRef: { current: 1 },
      setMessages,
    };
    const view = renderHook(() => {
      useWorkerReportRefresh(options);
    });
    expect(fetchThreadMessages).not.toHaveBeenCalled();
    reported = true;
    view.rerender();
    await waitFor(() => {
      expect(setMessages).toHaveBeenCalledTimes(1);
    });
    const update = setMessages.mock.calls[0]?.[0];
    if (typeof update !== 'function') throw new Error('expected a guarded snapshot update');
    expect(update([])).toEqual(messages);
    view.rerender();
    expect(fetchThreadMessages).toHaveBeenCalledTimes(1);
  });
  it('waits until the parent stream settles', async () => {
    const options = {
      threadId: 'conv',
      isRunning: true,
      historyRequestRef: { current: 1 },
      setMessages: vi.fn<(value: SetStateAction<ThreadMessageLike[]>) => void>(),
    };
    const view = renderHook(
      (props) => {
        useWorkerReportRefresh(props);
      },
      { initialProps: options },
    );
    expect(fetchThreadMessages).not.toHaveBeenCalled();
    view.rerender({ ...options, isRunning: false });
    await waitFor(() => {
      expect(options.setMessages).toHaveBeenCalledTimes(1);
    });
    const update = options.setMessages.mock.calls[0]?.[0];
    if (typeof update !== 'function') throw new Error('expected a guarded snapshot update');
    expect(update([])).toEqual(messages);
  });
  it('does not replace a newer send with a late snapshot', async () => {
    let resolve: ((value: ThreadMessageLike[]) => void) | undefined;
    vi.mocked(fetchThreadMessages).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const generation = { current: 1 },
      setMessages = vi.fn();
    renderHook(() => {
      useWorkerReportRefresh({
        threadId: 'conv',
        isRunning: false,
        historyRequestRef: generation,
        setMessages,
      });
    });
    generation.current += 1;
    await act(async () => {
      resolve?.(messages);
      await Promise.resolve();
    });
    expect(setMessages).not.toHaveBeenCalled();
  });
});

it('preserves distinct live IDs for repeated prompts while appending a durable report', () => {
  const user = (id: string): ThreadMessageLike => ({
    id,
    role: 'user',
    content: 'repeat the work',
  });
  const assistant = (id: string): ThreadMessageLike => ({
    id,
    role: 'assistant',
    content: 'started',
  });
  const before = [user('live-a'), assistant('run-a'), user('live-b'), assistant('run-b')];
  const after = [
    user('msg-1'),
    assistant('msg-2'),
    user('msg-5'),
    assistant('msg-6'),
    assistant('report-9'),
  ];
  expect(preserveMessageIDs(before, after).map((message) => message.id)).toEqual([
    'live-a',
    'run-a',
    'live-b',
    'run-b',
    'report-9',
  ]);
  expect(preserveMessageIDs(before, [...after, user('new-user')])).toBeInstanceOf(Array);
  expect(preserveMessageIDs(before, [...after, user('new-user')])[0]?.id).toBe('msg-1');
});
