import { act, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { WorkerPane } from './WorkerPane';
import { WorkerWatchContext, type WorkerWatchController } from './workerWatchControls';
import type { WorkerStatus, WorkerStreamHandlers } from './workerStream';

const streams: { handlers: WorkerStreamHandlers; close: ReturnType<typeof vi.fn> }[] = [];
vi.mock('./workerStream', () => ({
  openWorkerStream: (_conv: string, _child: string, handlers: WorkerStreamHandlers) => {
    const stream = { handlers, close: vi.fn() };
    streams.push(stream);
    return stream;
  },
}));
vi.mock('./WorkerControls', () => ({ WorkerControls: () => null }));

const status: WorkerStatus = {
  child_id: 'child',
  status: 'running',
  run_id: 'run-one',
  last_event_at: '',
  events: 1,
  duration_sec: 1,
};
const base: Omit<WorkerWatchController, 'statuses'> = {
  workers: [],
  registerWorkers: () => () => undefined,
  watchWorker: () => undefined,
  viewReport: () => undefined,
};
const content = (value: WorkerStatus) => (
  <WorkerWatchContext.Provider value={{ ...base, statuses: new Map([['child', value]]) }}>
    <WorkerPane conversationId="conversation" childId="child" onClose={vi.fn()} />
  </WorkerWatchContext.Provider>
);

beforeEach(() => {
  streams.length = 0;
});

describe('worker transcript lifetime', () => {
  it('keeps the live stream through completion metadata so its final answer remains visible', async () => {
    const view = render(content(status));
    const initial = streams[0];
    if (initial === undefined) throw new Error('missing initial stream');
    act(() => {
      initial.handlers.onMessages([
        {
          id: 'child',
          role: 'assistant',
          content: [{ type: 'reasoning', text: 'Checking output' }],
          status: { type: 'running' },
        },
      ]);
    });
    view.rerender(
      content({ child_id: 'child', status: 'ok', events: 4, duration_sec: 5, last_event_at: '' }),
    );
    expect(streams).toHaveLength(1);
    expect(initial.close).not.toHaveBeenCalled();
    act(() => {
      initial.handlers.onMessages([
        {
          id: 'child',
          role: 'assistant',
          content: [
            { type: 'reasoning', text: 'Checking output' },
            { type: 'text', text: '{"verificato":144,"nota":"second104D"}' },
          ],
          status: { type: 'complete', reason: 'stop' },
        },
      ]);
    });
    await waitFor(() => {
      expect(screen.getByText('{"verificato":144,"nota":"second104D"}')).toBeTruthy();
    });
  });

  it('reconnects for a new incarnation and ignores a delayed callback from the old one', async () => {
    const view = render(content(status));
    const initial = streams[0];
    view.rerender(content({ ...status, run_id: 'run-two' }));
    expect(streams).toHaveLength(2);
    expect(initial?.close).toHaveBeenCalledOnce();
    act(() =>
      streams[1]?.handlers.onMessages([
        {
          id: 'child',
          role: 'assistant',
          content: [{ type: 'text', text: 'New execution result' }],
          status: { type: 'complete', reason: 'stop' },
        },
      ]),
    );
    act(() =>
      initial?.handlers.onMessages([
        {
          id: 'child',
          role: 'assistant',
          content: [{ type: 'text', text: 'Stale execution result' }],
          status: { type: 'complete', reason: 'stop' },
        },
      ]),
    );
    await waitFor(() => {
      expect(screen.getByText('New execution result')).toBeTruthy();
    });
    expect(screen.queryByText('Stale execution result')).toBeNull();
  });
});
