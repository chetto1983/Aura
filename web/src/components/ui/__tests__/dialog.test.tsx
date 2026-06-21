import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '../dialog';

describe('Dialog', () => {
  it('renders a labelled modal dialog when open', () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogTitle>Install skill</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    const dlg = screen.getByRole('dialog');
    expect(dlg).toBeTruthy();
    expect(dlg.getAttribute('aria-labelledby')).toBeTruthy();
    expect(screen.getByText('Install skill')).toBeTruthy();
  });

  it('exposes a trigger that opens the modal on click', async () => {
    render(
      <Dialog>
        <DialogTrigger>Open</DialogTrigger>
        <DialogContent>
          <DialogTitle>Install skill</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    const trigger = screen.getByRole('button', { name: 'Open' });
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(trigger);
    expect(await screen.findByRole('dialog')).toBeTruthy();
  });

  it('composes header/footer/description and a labelled close button', () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Install skill</DialogTitle>
            <DialogDescription>Stage a skill for approval.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose>Discard</DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>,
    );
    const dlg = screen.getByRole('dialog');
    expect(dlg.getAttribute('aria-describedby')).toBeTruthy();
    expect(screen.getByText('Stage a skill for approval.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Discard' })).toBeTruthy();
    // The built-in ✕ keeps the 44px touch floor.
    expect(screen.getByRole('button', { name: 'Close' }).className).toContain('h-11');
  });

  it('omits the built-in close button when showCloseButton is false', () => {
    render(
      <Dialog open>
        <DialogContent showCloseButton={false}>
          <DialogTitle>No close</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull();
  });
});
