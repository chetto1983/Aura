import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '../../i18n/i18n';
import { DeleteConfirmDialog } from '../DeleteConfirmDialog';

describe('DeleteConfirmDialog (T-25-14)', () => {
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
    const dialog = screen.getByRole('dialog', { name: 'Delete conversation?' });
    expect(within(dialog).getByText(/permanently deletes "My run"/)).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Delete permanently' })).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Keep conversation' })).toBeTruthy();
  });

  it('renders through the shared centered portal shell', () => {
    const { container } = render(
      <DeleteConfirmDialog
        open
        title="My run"
        onConfirm={() => undefined}
        onCancel={() => undefined}
      />,
    );
    const dialog = screen.getByRole('dialog', { name: 'Delete conversation?' });
    expect(container.contains(dialog)).toBe(false);
    expect(dialog.className).toContain('left-[50%]');
    expect(dialog.className).toContain('top-[50%]');
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

  it('Escape routes to onCancel and safe cancel is default-focused', async () => {
    const onCancel = vi.fn();
    render(<DeleteConfirmDialog open title="X" onConfirm={() => undefined} onCancel={onCancel} />);
    const dialog = screen.getByRole('dialog', { name: 'Delete conversation?' });
    const cancel = within(dialog).getByRole('button', { name: 'Keep conversation' });
    await waitFor(() => {
      expect(document.activeElement).toBe(cancel);
    });

    fireEvent.keyDown(document, { key: 'Escape', code: 'Escape' });
    await waitFor(() => {
      expect(onCancel).toHaveBeenCalledTimes(1);
    });
  });
});
