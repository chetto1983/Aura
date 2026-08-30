import { act, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
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
const threadBWorkers: readonly DisplayChildReport[] = [
  { goal_index: 0, child_id: 'child-b', status: 'running', goal: 'Thread B work' },
];

function WorkerRegistration({
  registrationId,
  workers,
}: {
  readonly registrationId: string;
  readonly workers: readonly DisplayChildReport[];
}) {
  const { registerWorkers } = useWatchWorker();
  useEffect(
    () => registerWorkers(registrationId, workers),
    [registerWorkers, registrationId, workers],
  );
  return null;
}

function RegistryProbe() {
  const { workers } = useWatchWorker();
  return (
    <output aria-label="registered workers">
      {workers.map((worker) => worker.child_id).join(',')}
    </output>
  );
}

function ConversationRegistry({ showThreadA = true }: { readonly showThreadA?: boolean }) {
  return (
    <>
      {showThreadA ? (
        <WorkerRegistration registrationId="thread-a-card" workers={threadAWorkers} />
      ) : null}
      <WorkerRegistration registrationId="thread-b-card" workers={threadBWorkers} />
      <RegistryProbe />
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
        <WorkerRegistration registrationId="thread-a-card" workers={threadAWorkers} />
        <RegistryProbe />
      </WorkerWatchProvider>,
    );

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

  it('unions mounted swarm cards and removes only the card that unmounts', async () => {
    const view = render(
      <WorkerWatchProvider conversationId="thread-a" onWatchWorker={vi.fn()} onViewReport={vi.fn()}>
        <ConversationRegistry />
      </WorkerWatchProvider>,
    );

    expect(screen.getByLabelText('registered workers').textContent).toBe('child-a,child-b');

    view.rerender(
      <WorkerWatchProvider conversationId="thread-a" onWatchWorker={vi.fn()} onViewReport={vi.fn()}>
        <ConversationRegistry showThreadA={false} />
      </WorkerWatchProvider>,
    );

    await waitFor(() => {
      expect(screen.getByLabelText('registered workers').textContent).toBe('child-b');
    });
  });
});
