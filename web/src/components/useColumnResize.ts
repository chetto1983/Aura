import { useCallback, useEffect, useRef, useState } from 'react';

// useColumnResize — the drag-to-resize behind every left column that is NOT a chat panel:
// the governance boards' master list and the SectionRail sidebar.
//
// The chat rail uses react-resizable-panels, which is the right tool when panels must
// negotiate space between three siblings. These columns have a second regime: below `lg` the
// board detail becomes a fixed bottom sheet and the rail becomes a horizontal strip. A panel
// group cannot be either, and swapping trees at the breakpoint remounts the subtree. So the
// column stays a CSS track and this hook only moves the number: no wrapper, no remount, and
// the mobile layout is untouched because the track is inert there.
//
// The width is measured from the resized element's OWN left edge, never from the viewport's:
// a column with anything to its left (a rail, a gutter) would otherwise jump by that offset
// on the first pointer move. It is persisted per browser and clamped on read, so a stored
// value from a wider monitor cannot leave the pane beside it a sliver on a laptop.

export interface ColumnResizeOptions<T extends HTMLElement> {
  /**
   * The element whose LEFT EDGE the width is measured from. The caller owns it: a ref handed
   * BACK through a hook result travels through render, which is what `react-hooks/refs` forbids.
   */
  readonly originRef: React.RefObject<T | null>;
  /** localStorage key — one per column, or two columns would share a width. */
  readonly storageKey: string;
  readonly defaultWidth: number;
  readonly min: number;
  readonly max: number;
}

/** What the drag handle needs — element-type-free, so one handle serves every column. */
export interface ColumnResizeControl {
  /** Current width in px, for the CSS track. */
  readonly width: number;
  readonly min: number;
  readonly max: number;
  /** Pointer-down on the handle starts the drag. */
  readonly onPointerDown: (event: React.PointerEvent<HTMLElement>) => void;
  /** Arrow keys nudge, Home/End jump to the bounds — the handle is a real control. */
  readonly onKeyDown: (event: React.KeyboardEvent<HTMLElement>) => void;
}

export function useColumnResize<T extends HTMLElement>({
  originRef,
  storageKey,
  defaultWidth,
  min,
  max,
}: ColumnResizeOptions<T>): ColumnResizeControl {
  const clamp = useCallback(
    (px: number) => Math.min(max, Math.max(min, Math.round(px))),
    [max, min],
  );

  const [width, setWidth] = useState<number>(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      if (raw === null) {
        return defaultWidth;
      }
      const parsed = Number(raw);
      return Number.isFinite(parsed) ? clamp(parsed) : defaultWidth;
    } catch {
      return defaultWidth;
    }
  });

  const dragging = useRef(false);
  // The live width, so pointer-up can persist the last dragged value without reaching for it
  // through a setState updater (which React would double-invoke in strict mode).
  const widthRef = useRef(width);

  const persist = useCallback(
    (next: number) => {
      try {
        localStorage.setItem(storageKey, String(next));
      } catch {
        // Storage is optional; the live width still applies for this session.
      }
    },
    [storageKey],
  );

  const apply = useCallback((next: number) => {
    widthRef.current = next;
    setWidth(next);
  }, []);

  // The move/up listeners live on the document, not the handle: a fast drag outruns a 6px
  // target, and a handle that stops resizing the moment the pointer leaves it feels broken.
  useEffect(() => {
    function onMove(event: PointerEvent) {
      if (!dragging.current) {
        return;
      }
      event.preventDefault();
      const originLeft = originRef.current?.getBoundingClientRect().left ?? 0;
      apply(clamp(event.clientX - originLeft));
    }
    function onUp() {
      if (!dragging.current) {
        return;
      }
      dragging.current = false;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      persist(widthRef.current);
    }
    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
    return () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
    };
  }, [apply, clamp, originRef, persist]);

  const onPointerDown = useCallback((event: React.PointerEvent<HTMLElement>) => {
    event.preventDefault();
    dragging.current = true;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }, []);

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLElement>) => {
      const step = event.shiftKey ? 48 : 16;
      const next = {
        ArrowLeft: () => widthRef.current - step,
        ArrowRight: () => widthRef.current + step,
        Home: () => min,
        End: () => max,
      }[event.key];
      if (next === undefined) {
        return;
      }
      event.preventDefault();
      const value = clamp(next());
      apply(value);
      persist(value);
    },
    [apply, clamp, max, min, persist],
  );

  return { width, min, max, onPointerDown, onKeyDown };
}
