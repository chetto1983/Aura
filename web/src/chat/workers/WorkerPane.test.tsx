import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { WorkerStreamHandlers } from './workerStream';
import { WorkerPane } from './WorkerPane';

let handlers: WorkerStreamHandlers | undefined;
const close = vi.fn();

vi.mock('./workerStream', () => ({
  openWorkerStream: (_conversationId: string, _childId: string, next: WorkerStreamHandlers) => {
    handlers = next;
    return { close };
  },
}));

describe('WorkerPane', () => {
  beforeEach(() => {
    handlers = undefined;
    close.mockClear();
  });

  it('shows connecting copy, exposes a literal 44px close target, and closes the stream', () => {
    const onClose = vi.fn();
    const view = render(
      <WorkerPane conversationId="conv-1" childId="child-1" onClose={onClose} />,
    );

    expect(screen.getByText('Connecting to worker...')).toBeTruthy();
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
    expect(screen.queryByText('Connecting to worker...')).toBeNull();
  });
});
