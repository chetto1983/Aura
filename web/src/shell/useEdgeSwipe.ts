import { useRef, type PointerEventHandler } from 'react';

interface EdgeSwipeOptions {
  readonly onLeftEdge?: () => void;
  readonly onRightEdge?: () => void;
  readonly edgeSize?: number;
  readonly threshold?: number;
}

export function useEdgeSwipe({
  onLeftEdge,
  onRightEdge,
  edgeSize = 24,
  threshold = 56,
}: EdgeSwipeOptions) {
  const start = useRef<{ x: number; y: number } | null>(null);

  const onPointerDown: PointerEventHandler<HTMLElement> = (event) => {
    if (event.pointerType === 'mouse') return;
    const width = window.innerWidth;
    if (event.clientX <= edgeSize || event.clientX >= width - edgeSize) {
      start.current = { x: event.clientX, y: event.clientY };
    }
  };

  const onPointerUp: PointerEventHandler<HTMLElement> = (event) => {
    const initial = start.current;
    start.current = null;
    if (!initial) return;
    const dx = event.clientX - initial.x;
    const dy = Math.abs(event.clientY - initial.y);
    if (dy > threshold) return;
    if (initial.x <= edgeSize && dx > threshold) onLeftEdge?.();
    if (initial.x >= window.innerWidth - edgeSize && dx < -threshold) onRightEdge?.();
  };

  return { onPointerDown, onPointerUp };
}
