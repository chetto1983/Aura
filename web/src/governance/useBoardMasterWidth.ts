import { useCallback, useEffect, useRef, useState } from 'react';

// useBoardMasterWidth — the drag-to-resize behind the governance boards' master column.
//
// The chat rail uses react-resizable-panels, which is the right tool when panels must
// negotiate space between three siblings. Here there are two, one of which becomes a fixed
// bottom sheet below lg — a panel group cannot be that, and swapping trees at the
// breakpoint remounts the boards. So the column stays a CSS grid track and this hook only
// moves the number: no wrapper, no remount, and the mobile layout is untouched because the
// grid template is inert there.
//
// The width is persisted per browser, clamped on read: a stored value from a wider monitor
// must not leave the detail pane a sliver on a laptop.

const STORAGE_KEY = 'aura.governance.masterWidth';
const DEFAULT_WIDTH = 320;
const MIN_WIDTH = 240;
const MAX_WIDTH = 640;

function clamp(px: number): number {
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.round(px)));
}

function readStored(): number {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === null) {
      return DEFAULT_WIDTH;
    }
    const parsed = Number(raw);
    return Number.isFinite(parsed) ? clamp(parsed) : DEFAULT_WIDTH;
  } catch {
    return DEFAULT_WIDTH;
  }
}

export interface BoardMasterWidth {
  /** Current width in px, for the grid template. */
  readonly width: number;
  /** Pointer-down on the handle starts the drag. */
  readonly onPointerDown: (event: React.PointerEvent<HTMLElement>) => void;
  /** Arrow keys nudge, Home/End jump to the bounds — the handle is a real control. */
  readonly onKeyDown: (event: React.KeyboardEvent<HTMLElement>) => void;
  readonly min: number;
  readonly max: number;
}

export function useBoardMasterWidth(): BoardMasterWidth {
  const [width, setWidth] = useState<number>(readStored);
  const dragging = useRef(false);

  const persist = useCallback((next: number) => {
    try {
      localStorage.setItem(STORAGE_KEY, String(next));
    } catch {
      // Storage is optional; the live width still applies for this session.
    }
  }, []);

  // The move/up listeners live on the document, not the handle: a fast drag outruns a 6px
  // target, and a handle that stops resizing the moment the pointer leaves it feels broken.
  useEffect(() => {
    function onMove(event: PointerEvent) {
      if (!dragging.current) {
        return;
      }
      event.preventDefault();
      setWidth(clamp(event.clientX));
    }
    function onUp() {
      if (!dragging.current) {
        return;
      }
      dragging.current = false;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      setWidth((current) => {
        persist(current);
        return current;
      });
    }
    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
    return () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
    };
  }, [persist]);

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
        ArrowLeft: () => width - step,
        ArrowRight: () => width + step,
        Home: () => MIN_WIDTH,
        End: () => MAX_WIDTH,
      }[event.key];
      if (next === undefined) {
        return;
      }
      event.preventDefault();
      const value = clamp(next());
      setWidth(value);
      persist(value);
    },
    [persist, width],
  );

  return { width, onPointerDown, onKeyDown, min: MIN_WIDTH, max: MAX_WIDTH };
}
