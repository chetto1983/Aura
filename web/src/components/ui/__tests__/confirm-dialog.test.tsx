import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import { ConfirmDialog } from '../confirm-dialog';
import { Button } from '../button';

describe('ConfirmDialog', () => {
  it('opens from its composed trigger and restores focus after Cancel or Escape', async () => {
    function Example() {
      const [open, setOpen] = useState(false);
      return (
        <ConfirmDialog
          open={open}
          onOpenChange={setOpen}
          trigger={<Button>Delete saved view</Button>}
          title="Delete this saved view?"
          cancelLabel="Cancel"
          confirmLabel="Confirm"
          onConfirm={() => undefined}
        />
      );
    }
    render(<Example />);
    const trigger = screen.getByRole('button', { name: 'Delete saved view' });
    trigger.focus();
    fireEvent.click(trigger);
    const cancel = screen.getByRole('button', { name: 'Cancel' });
    await waitFor(() => {
      expect(document.activeElement).toBe(cancel);
    });
    fireEvent.click(cancel);
    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
    fireEvent.click(trigger);
    fireEvent.keyDown(screen.getByRole('button', { name: 'Cancel' }), { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
      expect(document.activeElement).toBe(trigger);
    });
  });

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
