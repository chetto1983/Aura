import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useRef, useState } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { BoardLayout } from '../BoardLayout';

// BoardLayout test — the master/detail shell. The desktop path (matchMedia → no match) renders the
// detail as a static column and shows the detail-empty copy when nothing is selected. The MOBILE
// path (matchMedia → match) is the bottom sheet: it traps focus (Tab cycles inside, Escape closes),
// the backdrop taps to dismiss, and focus RESTORES to the originating row on close (28-UI-SPEC
// §A11y). matchMedia is overridden per-test to drive each viewport.

function setViewport(isMobile: boolean) {
  window.matchMedia = (query: string) => ({
    matches: isMobile,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
}

// A small harness exercising the BoardLayout open/close contract with a real originating button
// (so focus-restore is observable) and a detail pane with two focusables (so Tab cycling is real).
function Harness() {
  const [open, setOpen] = useState(false);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  return (
    <div>
      <button
        type="button"
        onClick={(e) => {
          restoreFocusRef.current = e.currentTarget;
          setOpen(true);
        }}
      >
        open-row
      </button>
      <BoardLayout
        master={<div>master-list</div>}
        detail={
          open ? (
            <div className="p-4">
              <button type="button">detail-first</button>
              <button type="button">detail-last</button>
            </div>
          ) : undefined
        }
        detailOpen={open}
        onCloseDetail={() => {
          setOpen(false);
        }}
        restoreFocusRef={restoreFocusRef}
        detailLabel="detail-region"
      />
    </div>
  );
}

describe('BoardLayout', () => {
  beforeEach(() => {
    setViewport(false);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the detail-empty copy on desktop when nothing is selected', () => {
    render(
      <BoardLayout
        master={<div>master-list</div>}
        detail={undefined}
        detailOpen={false}
        onCloseDetail={() => undefined}
        restoreFocusRef={{ current: null }}
        detailLabel="detail-region"
      />,
    );
    expect(screen.getByText('Select a row to see details')).toBeTruthy();
    expect(screen.getByText('master-list')).toBeTruthy();
  });

  it('on mobile, opening the sheet moves focus inside and Escape closes + restores focus', async () => {
    setViewport(true);
    render(<Harness />);

    const opener = screen.getByRole('button', { name: 'open-row' });
    opener.focus();
    fireEvent.click(opener);

    // Focus moves into the sheet (first focusable).
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'detail-first' }));
    });

    // Escape closes the sheet and restores focus to the originating row.
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'detail-first' })).toBeNull();
    });
    expect(document.activeElement).toBe(opener);
  });

  it('on mobile, Tab from the last focusable wraps to the first (focus trap)', async () => {
    setViewport(true);
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: 'open-row' }));

    const last = await screen.findByRole('button', { name: 'detail-last' });
    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'detail-first' }));
    });
  });

  it('on mobile, Shift+Tab from the first focusable wraps to the last', async () => {
    setViewport(true);
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: 'open-row' }));

    const first = await screen.findByRole('button', { name: 'detail-first' });
    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'detail-last' }));
    });
  });

  it('on mobile, a Tab in the middle of the trap does not force-wrap (only the edges wrap)', async () => {
    setViewport(true);
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: 'open-row' }));

    // Focus the first; a Tab from a non-edge element is not intercepted (no preventDefault wrap).
    const first = await screen.findByRole('button', { name: 'detail-first' });
    first.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    // Focus stays on first (the browser would move it; jsdom does not, and the trap did NOT wrap
    // it to last because first is the trap's first edge only for shift+Tab).
    expect(document.activeElement).toBe(first);
  });

  it('on mobile, a non-Tab/non-Escape key inside the sheet is ignored by the trap', async () => {
    setViewport(true);
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: 'open-row' }));
    await screen.findByRole('button', { name: 'detail-first' });

    // An arbitrary key must not close the sheet (only Escape closes).
    fireEvent.keyDown(document, { key: 'a' });
    expect(screen.getByRole('button', { name: 'detail-first' })).toBeTruthy();
  });

  it('the backdrop taps to dismiss the sheet', async () => {
    setViewport(true);
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: 'open-row' }));
    await screen.findByRole('button', { name: 'detail-first' });

    // The backdrop is the close-labelled button overlaying the sheet.
    const backdrops = screen.getAllByRole('button', { name: 'Close' });
    const backdrop = backdrops[backdrops.length - 1];
    if (backdrop === undefined) throw new Error('backdrop not rendered');
    fireEvent.click(backdrop);
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'detail-first' })).toBeNull();
    });
  });
  it('the master column resizes by keyboard and remembers the width', () => {
    setViewport(false);
    localStorage.removeItem('aura.governance.masterWidth');
    const { unmount } = render(<Harness />);

    const handle = screen.getByRole('separator', { name: 'Resize the list column' });
    const before = Number(handle.getAttribute('aria-valuenow'));
    fireEvent.keyDown(handle, { key: 'ArrowRight' });
    const after = Number(handle.getAttribute('aria-valuenow'));
    expect(after).toBeGreaterThan(before);
    expect(localStorage.getItem('aura.governance.masterWidth')).toBe(String(after));

    // The width survives a remount — a column you had to widen once should not snap back
    // every time you open the board.
    unmount();
    render(<Harness />);
    expect(
      screen
        .getByRole('separator', { name: 'Resize the list column' })
        .getAttribute('aria-valuenow'),
    ).toBe(String(after));
  });

  it('clamps a stored width so a narrow viewport never loses the detail pane', () => {
    setViewport(false);
    localStorage.setItem('aura.governance.masterWidth', '5000');
    render(<Harness />);
    const handle = screen.getByRole('separator', { name: 'Resize the list column' });
    expect(Number(handle.getAttribute('aria-valuenow'))).toBe(
      Number(handle.getAttribute('aria-valuemax')),
    );
  });
});
