import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { ConfirmDialog } from '../confirm-dialog';

describe('ConfirmDialog', () => {
  it('renders a centered portal dialog with the safe action default-focused', async () => {
    const onOpenChange = vi.fn();
    const { container } = render(
      <ConfirmDialog
        open
        onOpenChange={onOpenChange}
        title="Delete item?"
        description="This cannot be undone."
        cancelLabel="Keep item"
        confirmLabel="Delete item"
        onConfirm={() => undefined}
      />,
    );

    const dialog = screen.getByRole('dialog', { name: 'Delete item?' });
    expect(container.contains(dialog)).toBe(false);
    expect(dialog.className).toContain('left-[50%]');
    expect(dialog.className).toContain('top-[50%]');
    expect(dialog.className).toContain('translate-x-[-50%]');
    expect(dialog.className).toContain('translate-y-[-50%]');
    await waitFor(() => {
      expect(document.activeElement).toBe(
        within(dialog).getByRole('button', { name: 'Keep item' }),
      );
    });

    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep item' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('can render destructive confirmations as alertdialogs', () => {
    render(
      <ConfirmDialog
        open
        role="alertdialog"
        onOpenChange={() => undefined}
        title="Remove server?"
        cancelLabel="Keep server"
        confirmLabel="Remove server"
        onConfirm={() => undefined}
      />,
    );

    expect(screen.getByRole('alertdialog', { name: 'Remove server?' })).toBeTruthy();
  });

  it('disables the confirm button while blocked or pending', () => {
    render(
      <ConfirmDialog
        open
        onOpenChange={() => undefined}
        title="Delete item?"
        cancelLabel="Keep item"
        confirmLabel="Delete item"
        confirmDisabled
        onConfirm={() => undefined}
      />,
    );

    expect(screen.getByRole('button', { name: 'Delete item' }).hasAttribute('disabled')).toBe(true);
  });
});
