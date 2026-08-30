import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect, useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { DisplayChildReport } from '../displays/types';
import type { WorkerStreamHandlers } from './workerStream';
import { WorkerPane } from './WorkerPane';
import { useWatchWorker } from './workerWatchControls';
import { WorkerWatchProvider } from './WorkerWatchProvider';

let handlers: WorkerStreamHandlers | undefined;
const close = vi.fn();
const openWorkerStream = vi.fn(
  (_conversationId: string, _childId: string, next: WorkerStreamHandlers) => {
    handlers = next;
    return { close };
  },
);

vi.mock('./workerStream', () => ({
  openWorkerStream: (...args: Parameters<typeof openWorkerStream>) => openWorkerStream(...args),
}));

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

function OwnedPaneProbe({ conversationId }: { readonly conversationId: string }) {
  const [open, setOpen] = useState(false);
  const { watchWorker } = useWatchWorker();
  const workers = conversationId === 'thread-a' ? threadAWorkers : threadBWorkers;
  return (
    <>
      <WorkerRegistration registrationId="owned-pane" workers={workers} />
      {open ? (
        <WorkerPane
          conversationId={conversationId}
          childId="child-a"
          onClose={() => {
            setOpen(false);
          }}
        />
      ) : (
        <button
          type="button"
          onClick={() => {
            watchWorker('child-a');
            setOpen(true);
          }}
        >
          Open child A
        </button>
      )}
    </>
  );
}

describe('WorkerPane', () => {
  beforeEach(() => {
    handlers = undefined;
    close.mockClear();
    openWorkerStream.mockClear();
  });

  it('shows connecting copy, exposes a literal 44px close target, and closes the stream', () => {
    const onClose = vi.fn();
    const view = render(<WorkerPane conversationId="conv-1" childId="child-1" onClose={onClose} />);

    expect(screen.getByText('Connecting to worker…')).toBeTruthy();
    const closeButton = screen.getByRole('button', { name: 'Close worker pane' });
    expect(closeButton.className).toContain('min-h-[44px]');
    expect(closeButton.className).toContain('min-w-[44px]');
    expect(view.container.querySelector('form')).toBeNull();

    fireEvent.click(closeButton);
    expect(onClose).toHaveBeenCalledTimes(1);
    view.unmount();
    expect(close).toHaveBeenCalledTimes(1);
  });

  it('replaces connecting copy with the report-artifact fallback on stream error', () => {
    render(<WorkerPane conversationId="conv-1" childId="child-1" onClose={vi.fn()} />);

    act(() => {
      handlers?.onError();
    });

    expect(screen.getByRole('alert').textContent).toContain('check the report artifact');
    expect(screen.queryByText('Connecting to worker…')).toBeNull();
  });

  it('renders the reduced read-only message once the first part arrives', () => {
    render(<WorkerPane conversationId="conv-1" childId="child-1" onClose={vi.fn()} />);

    act(() => {
      handlers?.onMessages([
        {
          id: 'child-1',
          role: 'assistant',
          content: [{ type: 'text', text: 'Worker result' }],
          status: { type: 'complete', reason: 'stop' },
        },
      ]);
    });

    expect(screen.getByText('Worker result')).toBeTruthy();
    expect(screen.queryByText('Connecting to worker…')).toBeNull();
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it('does not request or display a persisted thread A child under thread B', async () => {
    const onClose = vi.fn();
    render(
      <WorkerWatchProvider conversationId="thread-b" onWatchWorker={vi.fn()} onViewReport={vi.fn()}>
        <WorkerRegistration registrationId="thread-b-card" workers={threadBWorkers} />
        <WorkerPane conversationId="thread-b" childId="child-a" onClose={onClose} />
      </WorkerWatchProvider>,
    );

    expect(openWorkerStream).not.toHaveBeenCalled();
    expect(screen.queryByText('child-a')).toBeNull();
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it('waits for delayed registry hydration before restoring an owned worker', async () => {
    const onClose = vi.fn();

    function DelayedRegistry() {
      const [ready, setReady] = useState(false);
      return (
        <>
          <button
            type="button"
            onClick={() => {
              setReady(true);
            }}
          >
            Hydrate workers
          </button>
          {ready ? (
            <WorkerRegistration registrationId="delayed-card" workers={threadAWorkers} />
          ) : null}
          <WorkerPane conversationId="thread-a" childId="child-a" onClose={onClose} />
        </>
      );
    }

    render(
      <WorkerWatchProvider conversationId="thread-a" onWatchWorker={vi.fn()} onViewReport={vi.fn()}>
        <DelayedRegistry />
      </WorkerWatchProvider>,
    );

    expect(openWorkerStream).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Hydrate workers' }));

    await waitFor(() => {
      expect(openWorkerStream).toHaveBeenCalledWith('thread-a', 'child-a', expect.any(Object));
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  it('closes child A without requesting it under thread B when the conversation changes', async () => {
    const view = render(
      <WorkerWatchProvider conversationId="thread-a" onWatchWorker={vi.fn()} onViewReport={vi.fn()}>
        <OwnedPaneProbe conversationId="thread-a" />
      </WorkerWatchProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Open child A' }));
    expect(screen.getByText('child-a')).toBeTruthy();
    expect(openWorkerStream).toHaveBeenCalledWith('thread-a', 'child-a', expect.any(Object));

    view.rerender(
      <WorkerWatchProvider conversationId="thread-b" onWatchWorker={vi.fn()} onViewReport={vi.fn()}>
        <OwnedPaneProbe conversationId="thread-b" />
      </WorkerWatchProvider>,
    );

    expect(screen.queryByText('child-a')).toBeNull();
    await waitFor(() => {
      expect(close).toHaveBeenCalledTimes(1);
      expect(screen.getByRole('button', { name: 'Open child A' })).toBeTruthy();
    });
    expect(openWorkerStream).toHaveBeenCalledTimes(1);
  });
});
