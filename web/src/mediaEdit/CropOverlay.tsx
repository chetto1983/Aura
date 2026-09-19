import { useRef, type KeyboardEvent, type PointerEvent } from 'react';
import { moveRect, type CropRect, type Size } from './cropMath';

// The crop box drawn over the preview. It moves, it does not resize (the presets set the size).
// Positions are percentages of the frame, so the box follows the preview at any size; a drag is
// converted back to frame pixels through the parent's rendered width. A <button>, like
// ColumnResizeHandle: a natively focusable element keeps the arrow-key contract reachable.

function percent(part: number, whole: number): string {
  return `${String((part / whole) * 100)}%`;
}

export function CropOverlay({
  frame,
  rect,
  onMove,
  label,
}: {
  readonly frame: Size;
  readonly rect: CropRect;
  readonly onMove: (rect: CropRect) => void;
  readonly label: string;
}) {
  const last = useRef<{ readonly x: number; readonly y: number } | undefined>(undefined);

  function drag(event: PointerEvent<HTMLButtonElement>) {
    const from = last.current;
    const box = event.currentTarget.parentElement?.getBoundingClientRect();
    if (from === undefined || box === undefined || box.width === 0 || box.height === 0) return;
    last.current = { x: event.clientX, y: event.clientY };
    onMove(
      moveRect(
        rect,
        ((event.clientX - from.x) * frame.width) / box.width,
        ((event.clientY - from.y) * frame.height) / box.height,
        frame,
      ),
    );
  }

  function keys(event: KeyboardEvent<HTMLButtonElement>) {
    const step = event.shiftKey ? 50 : 10;
    const moves: Readonly<Record<string, readonly [number, number]>> = {
      ArrowLeft: [-step, 0],
      ArrowRight: [step, 0],
      ArrowUp: [0, -step],
      ArrowDown: [0, step],
    };
    const move = moves[event.key];
    if (move === undefined) return;
    event.preventDefault();
    onMove(moveRect(rect, move[0], move[1], frame));
  }

  return (
    <button
      type="button"
      aria-label={label}
      data-testid="crop-box"
      style={{
        left: percent(rect.left, frame.width),
        top: percent(rect.top, frame.height),
        width: percent(rect.width, frame.width),
        height: percent(rect.height, frame.height),
      }}
      className="absolute cursor-move touch-none border-2 border-accent shadow-[0_0_0_9999px_rgb(0_0_0/0.55)] focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        last.current = { x: event.clientX, y: event.clientY };
      }}
      onPointerMove={drag}
      onPointerUp={() => {
        last.current = undefined;
      }}
      onKeyDown={keys}
    />
  );
}
