import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { CropOverlay } from '../CropOverlay';

const FRAME = { width: 1280, height: 720 };
const SQUARE = { left: 280, top: 0, width: 720, height: 720 };

beforeAll(() => {
  // jsdom implements no pointer capture.
  HTMLElement.prototype.setPointerCapture = vi.fn();
});

function mount() {
  const onMove = vi.fn();
  render(
    <div data-testid="preview">
      <CropOverlay frame={FRAME} rect={SQUARE} onMove={onMove} label="Crop area" />
    </div>,
  );
  return { onMove, box: screen.getByRole('button', { name: 'Crop area' }) };
}

describe('CropOverlay', () => {
  it('places the box as percentages of the frame', () => {
    const { box } = mount();
    expect(box.style.left).toBe('21.875%');
    expect(box.style.top).toBe('0%');
    expect(box.style.width).toBe('56.25%');
    expect(box.style.height).toBe('100%');
  });

  it('moves ten pixels per arrow and fifty with Shift, never out of the frame', () => {
    const { onMove, box } = mount();
    fireEvent.keyDown(box, { key: 'ArrowRight' });
    fireEvent.keyDown(box, { key: 'ArrowLeft', shiftKey: true });
    fireEvent.keyDown(box, { key: 'ArrowUp' });
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onMove.mock.calls).toEqual([
      [{ ...SQUARE, left: 290 }],
      [{ ...SQUARE, left: 230 }],
      [SQUARE],
      [SQUARE],
    ]);
  });

  it('turns a drag on the preview into frame pixels', () => {
    const { onMove, box } = mount();
    vi.spyOn(screen.getByTestId('preview'), 'getBoundingClientRect').mockReturnValue({
      left: 0,
      top: 0,
      width: 640,
      height: 360,
      right: 640,
      bottom: 360,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    fireEvent.pointerMove(box, { clientX: 150, clientY: 100 });
    expect(onMove).not.toHaveBeenCalled();
    fireEvent.pointerDown(box, { clientX: 100, clientY: 100 });
    fireEvent.pointerMove(box, { clientX: 150, clientY: 100 });
    fireEvent.pointerMove(box, { clientX: 140, clientY: 100 });
    expect(onMove.mock.calls).toEqual([[{ ...SQUARE, left: 380 }], [{ ...SQUARE, left: 260 }]]);
    fireEvent.pointerUp(box);
    fireEvent.pointerMove(box, { clientX: 200, clientY: 100 });
    expect(onMove).toHaveBeenCalledTimes(2);
  });

  it('ignores a drag while the preview has no size', () => {
    const { onMove, box } = mount();
    fireEvent.pointerDown(box, { clientX: 100, clientY: 100 });
    fireEvent.pointerMove(box, { clientX: 150, clientY: 100 });
    expect(onMove).not.toHaveBeenCalled();
  });
});
