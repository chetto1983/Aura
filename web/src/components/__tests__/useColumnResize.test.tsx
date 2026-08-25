import { useRef } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ColumnResizeHandle } from '../ColumnResizeHandle';
import { useColumnResize } from '../useColumnResize';

// The drag path of useColumnResize, which the keyboard-only suites never reach. The case that
// matters is the ORIGIN: a column with a rail to its left is not at viewport x=0, so a width
// taken straight from `event.clientX` jumps by the rail's width on the first pointer move.

const OPTIONS = { storageKey: 'aura.test.width', defaultWidth: 240, min: 208, max: 448 };

function Harness() {
  const originRef = useRef<HTMLDivElement | null>(null);
  const resize = useColumnResize({ originRef, ...OPTIONS });
  return (
    <div ref={originRef} data-testid="origin">
      <ColumnResizeHandle resize={resize} label="Resize" />
    </div>
  );
}

function stubOriginLeft(left: number) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    left,
    top: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    x: left,
    y: 0,
    toJSON: () => ({}),
  });
}

function drag(handle: HTMLElement, clientX: number) {
  fireEvent.pointerDown(handle);
  fireEvent(document, new MouseEvent('pointermove', { clientX, bubbles: true }));
}

describe('useColumnResize', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it('measures the drag from the origin element, not from the viewport edge', () => {
    stubOriginLeft(240);
    render(<Harness />);
    const handle = screen.getByRole('separator', { name: 'Resize' });

    drag(handle, 500);

    // 500 - 240. Measured from the viewport this would have clamped to the 448 maximum —
    // the column would jump the width of whatever sits to its left.
    expect(handle.getAttribute('aria-valuenow')).toBe('260');
  });

  it('persists the dragged width on pointer-up and clamps to the bounds', () => {
    stubOriginLeft(0);
    render(<Harness />);
    const handle = screen.getByRole('separator', { name: 'Resize' });

    drag(handle, 300);
    expect(localStorage.getItem('aura.test.width')).toBeNull(); // nothing stored mid-drag
    fireEvent(document, new MouseEvent('pointerup', { bubbles: true }));
    expect(localStorage.getItem('aura.test.width')).toBe('300');

    drag(handle, 9000);
    expect(handle.getAttribute('aria-valuenow')).toBe('448');
  });

  it('carries a hit area wider than its hairline', () => {
    stubOriginLeft(0);
    render(<Harness />);
    // Live finding: with only `w-px` the pointer lands on the pane beside the handle and the
    // drag never starts. The visible line stays 1px; the pseudo-element takes the hits.
    const className = screen.getByRole('separator', { name: 'Resize' }).className;
    expect(className).toContain('lg:w-px');
    expect(className).toContain('after:w-3');
    // ...and above the pane on its right, which is itself positioned and later in the DOM.
    expect(className).toContain('z-10');
  });

  it('ignores pointer moves that are not part of a drag', () => {
    stubOriginLeft(0);
    render(<Harness />);
    const handle = screen.getByRole('separator', { name: 'Resize' });

    fireEvent(document, new MouseEvent('pointermove', { clientX: 400, bubbles: true }));

    expect(handle.getAttribute('aria-valuenow')).toBe('240');
  });
});
