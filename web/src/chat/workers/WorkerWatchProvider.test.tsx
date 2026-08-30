import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { DisplayChildReport } from '../displays/types';
import { useWatchWorker } from './workerWatchControls';
import { WorkerWatchProvider } from './WorkerWatchProvider';

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly url: string;
  closed = false;

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.instances.push(this);
  }

  addEventListener(): void {
    return undefined;
  }
  removeEventListener(): void {
    return undefined;
  }

  close(): void {
    this.closed = true;
  }
}

const threadAWorkers: readonly DisplayChildReport[] = [
  { goal_index: 0, child_id: 'child-a', status: 'running', goal: 'Thread A work' },
];

function RegistryProbe() {
  const { registerWorkers, workers } = useWatchWorker();
  return (
    <>
      <button
        type="button"
        onClick={() => {
          registerWorkers(threadAWorkers);
        }}
      >
        Register A
      </button>
      <output>{workers.map((worker) => worker.child_id).join(',')}</output>
    </>
  );
}

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal('EventSource', FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('WorkerWatchProvider conversation scope', () => {
  it('drops thread A workers and closes its status subscription when thread B becomes active', async () => {
    const view = render(
      <WorkerWatchProvider conversationId="thread-a" onWatchWorker={vi.fn()} onViewReport={vi.fn()}>
        <RegistryProbe />
      </WorkerWatchProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Register A' }));
    expect(screen.getByText('child-a')).toBeTruthy();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0]?.url).toBe('/api/conversations/thread-a/swarm/events');

    act(() => {
      view.rerender(
        <WorkerWatchProvider
          conversationId="thread-b"
          onWatchWorker={vi.fn()}
          onViewReport={vi.fn()}
        >
          <RegistryProbe />
        </WorkerWatchProvider>,
      );
    });

    expect(screen.queryByText('child-a')).toBeNull();
    await waitFor(() => {
      expect(FakeEventSource.instances[0]?.closed).toBe(true);
    });
    expect(FakeEventSource.instances).toHaveLength(1);
  });
});
