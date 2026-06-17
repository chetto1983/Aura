import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { DeleteConfirmDialog } from '../DeleteConfirmDialog';

describe('DeleteConfirmDialog (T-25-14)', () => {
  beforeEach(() => {
    if (typeof HTMLDialogElement !== 'undefined') {
      HTMLDialogElement.prototype.showModal = function show() {
        this.open = true;
      };
      HTMLDialogElement.prototype.close = function close() {
        this.open = false;
      };
    }
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the title interpolated into the body and the confirm/cancel verbs', () => {
    render(
      <DeleteConfirmDialog
        open
        title="My run"
        onConfirm={() => undefined}
        onCancel={() => undefined}
      />,
    );
    expect(screen.getByText('Delete conversation?')).toBeTruthy();
    expect(screen.getByText(/permanently deletes "My run"/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Delete permanently' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Keep conversation' })).toBeTruthy();
  });

  it('confirm calls onConfirm; cancel calls onCancel', () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(<DeleteConfirmDialog open title="X" onConfirm={onConfirm} onCancel={onCancel} />);
    fireEvent.click(screen.getByRole('button', { name: 'Delete permanently' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Keep conversation' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('the native cancel event (Esc/backdrop) routes to onCancel', () => {
    const onCancel = vi.fn();
    render(<DeleteConfirmDialog open title="X" onConfirm={() => undefined} onCancel={onCancel} />);
    fireEvent(screen.getByRole('dialog'), new Event('cancel', { cancelable: true }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('restores focus to the previously-focused element on close', () => {
    const trigger = document.createElement('button');
    document.body.appendChild(trigger);
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    const { rerender } = render(
      <DeleteConfirmDialog open title="X" onConfirm={() => undefined} onCancel={() => undefined} />,
    );
    // Opening moved focus into the dialog (Cancel button is the default focus).
    expect(document.activeElement).not.toBe(trigger);

    // Closing (open → false) closes the dialog and restores focus to the trigger.
    rerender(
      <DeleteConfirmDialog
        open={false}
        title="X"
        onConfirm={() => undefined}
        onCancel={() => undefined}
      />,
    );
    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });
});
