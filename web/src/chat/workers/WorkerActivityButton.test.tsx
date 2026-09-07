import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { WorkerActivityButton } from './WorkerActivityButton';
import { WorkerWatchContext, type WorkerWatchController } from './workerWatchControls';

function controller(overrides: Partial<WorkerWatchController> = {}): WorkerWatchController {
  return {
    workers: [],
    statuses: new Map(),
    registerWorkers: () => () => undefined,
    watchWorker: vi.fn(),
    viewReport: vi.fn(),
    ...overrides,
  };
}

describe('WorkerActivityButton', () => {
  it('stays absent in a conversation without workers', () => {
    render(<WorkerActivityButton />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('opens an active worker using live status instead of the old spawn result', () => {
    const controls = controller({
      workers: [
        { child_id: 'completed', goal_index: 0, status: 'running' },
        { child_id: 'paused', goal_index: 1, status: 'needs_user_input' },
      ],
      statuses: new Map([
        [
          'completed',
          { child_id: 'completed', status: 'ok', duration_sec: 10, events: 2, last_event_at: '' },
        ],
      ]),
    });
    render(
      <WorkerWatchContext.Provider value={controls}>
        <WorkerActivityButton />
      </WorkerWatchContext.Provider>,
    );
    expect(screen.getByText('1 active')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Open agent activity' }));
    expect(controls.watchWorker).toHaveBeenCalledWith('paused');
  });

  it('retains transcript access when all children are finished', () => {
    const controls = controller({ workers: [{ child_id: 'done', goal_index: 0, status: 'ok' }] });
    render(
      <WorkerWatchContext.Provider value={controls}>
        <WorkerActivityButton />
      </WorkerWatchContext.Provider>,
    );
    expect(screen.queryByText(/active/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Open agent activity' }));
    expect(controls.watchWorker).toHaveBeenCalledWith('done');
  });
});
