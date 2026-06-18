import { useCallback, useEffect, useRef } from 'react';

/**
 * Edge-swipe gesture layer for the off-canvas drawers (§3.1b of
 * docs/cockpit-overhaul/02-shell-sidebar-SPEC.md). Returns a `ref` callback meant to be
 * spread onto the shell root (`<div {...edgeSwipe}>`); it attaches NON-PASSIVE,
 * CAPTURE-PHASE touch listeners directly to that element so `preventDefault()` can claim
 * a horizontal gesture from the browser's native back-swipe / overscroll. React synthetic
 * handlers are passive + bubble-phase and cannot do this, hence raw listeners on a ref.
 */
interface EdgeSwipeOptions {
  readonly onLeftEdge?: () => void;
  readonly onRightEdge?: () => void;
  readonly onLeftClose?: () => void;
  readonly onRightClose?: () => void;
  readonly isLeftOpen?: boolean;
  readonly isRightOpen?: boolean;
  readonly edgeSize?: number;
}

interface EdgeSwipeHandlers {
  readonly ref: (node: HTMLElement | null) => void;
}

const ARM_TRAVEL = 10; // px before the horizontal-lock decision is taken (§3.1b.2)
const VERTICAL_ABORT = 40; // px of vertical drift that aborts an armed horizontal drag
const FLING_VELOCITY = 0.5; // px/ms release velocity that commits below the 50% line
const EXCLUDE_SELECTOR =
  'pre, code, table, input, textarea, [contenteditable], [role="dialog"], [data-drawer], [data-no-swipe]';

type Kind = 'open-left' | 'open-right' | 'close-left' | 'close-right';

interface Tracking {
  readonly kind: Kind;
  readonly startX: number;
  readonly startY: number;
  locked: boolean;
  bailed: boolean;
  lastX: number;
  lastT: number;
  prevX: number;
  prevT: number;
}

function point(event: TouchEvent): { x: number; y: number } | null {
  const t = event.touches[0] ?? event.changedTouches[0];
  return t ? { x: t.clientX, y: t.clientY } : null;
}

function isExcluded(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(EXCLUDE_SELECTOR) !== null;
}

export function useEdgeSwipe(options: EdgeSwipeOptions): EdgeSwipeHandlers {
  const opts = useRef(options);
  // Keep the latest options visible to the event-handler closures without re-binding the
  // raw listeners every render. Synced in an effect (not during render — refs must not be
  // written while rendering); handlers only ever read `opts.current` at event time.
  useEffect(() => {
    opts.current = options;
  });
  const track = useRef<Tracking | null>(null);
  const nodeRef = useRef<HTMLElement | null>(null);

  // The committed panel width drives the 50% commit threshold; it mirrors the Drawer's
  // `w-[min(22rem,88vw)]` so the half-open point matches the rendered panel.
  const panelWidth = useCallback(() => Math.min(352, window.innerWidth * 0.88), []);

  const begin = useCallback((event: TouchEvent) => {
    const p = point(event);
    if (!p || isExcluded(event.target)) return;
    const o = opts.current;
    const edge = o.edgeSize ?? 24;
    const width = window.innerWidth;
    const now = Date.now();
    let kind: Kind | null = null;
    if (o.isLeftOpen && o.onLeftClose) kind = 'close-left';
    else if (o.isRightOpen && o.onRightClose) kind = 'close-right';
    else if (o.onLeftEdge && p.x <= edge) kind = 'open-left';
    else if (o.onRightEdge && p.x >= width - edge) kind = 'open-right';
    if (!kind) return;
    track.current = {
      kind,
      startX: p.x,
      startY: p.y,
      locked: false,
      bailed: false,
      lastX: p.x,
      lastT: now,
      prevX: p.x,
      prevT: now,
    };
  }, []);

  const move = useCallback((event: TouchEvent) => {
    const g = track.current;
    if (!g || g.bailed) return;
    const p = point(event);
    if (!p) return;
    const dx = p.x - g.startX;
    const dy = p.y - g.startY;
    const now = Date.now();
    g.prevX = g.lastX;
    g.prevT = g.lastT;
    g.lastX = p.x;
    g.lastT = now;

    if (!g.locked) {
      if (Math.hypot(dx, dy) < ARM_TRAVEL) return; // wait for the arm threshold
      if (Math.abs(dx) <= Math.abs(dy)) {
        g.bailed = true; // vertical gesture — let the scrollers own it, never preventDefault
        return;
      }
      g.locked = true; // horizontal — claim it from the browser back-swipe / overscroll
    }
    if (Math.abs(dy) > VERTICAL_ABORT) {
      g.bailed = true; // diagonal scroll drifted off — release the gesture
      return;
    }
    if (event.cancelable) event.preventDefault();
  }, []);

  const end = useCallback(
    (event: TouchEvent) => {
      const g = track.current;
      track.current = null;
      if (!g || !g.locked || g.bailed) return;
      const p = point(event);
      if (!p) return;
      const dx = p.x - g.startX;
      // Velocity is the speed over the last real move segment; a same-tick synthetic
      // move (segment time 0) is NOT a fling — only genuine motion-over-time commits early.
      const segDt = g.lastT - g.prevT;
      const velocity = segDt > 0 ? (g.lastX - g.prevX) / segDt : 0;
      const half = panelWidth() / 2;
      const o = opts.current;

      switch (g.kind) {
        case 'open-left':
          if (dx > half || velocity > FLING_VELOCITY) o.onLeftEdge?.();
          break;
        case 'open-right':
          if (-dx > half || -velocity > FLING_VELOCITY) o.onRightEdge?.();
          break;
        case 'close-left':
          if (-dx > half || -velocity > FLING_VELOCITY) o.onLeftClose?.();
          break;
        case 'close-right':
          if (dx > half || velocity > FLING_VELOCITY) o.onRightClose?.();
          break;
      }
    },
    [panelWidth],
  );

  const attach = useCallback(
    (node: HTMLElement) => {
      const cap: AddEventListenerOptions = { capture: true, passive: false };
      node.addEventListener('touchstart', begin as EventListener, cap);
      node.addEventListener('touchmove', move as EventListener, cap);
      node.addEventListener('touchend', end as EventListener, cap);
      node.addEventListener('touchcancel', end as EventListener, cap);
      return () => {
        node.removeEventListener('touchstart', begin as EventListener, cap);
        node.removeEventListener('touchmove', move as EventListener, cap);
        node.removeEventListener('touchend', end as EventListener, cap);
        node.removeEventListener('touchcancel', end as EventListener, cap);
      };
    },
    [begin, move, end],
  );

  const detach = useRef<(() => void) | null>(null);

  const ref = useCallback(
    (node: HTMLElement | null) => {
      detach.current?.();
      detach.current = null;
      track.current = null;
      nodeRef.current = node;
      if (node) detach.current = attach(node);
    },
    [attach],
  );

  // Re-bind if the handler identities change (attach closes over begin/move/end) and
  // guarantee teardown on unmount so no listener leaks. The cleanup is always registered
  // (even when no node is bound yet) so a node attached imperatively after first render
  // is still detached on unmount.
  useEffect(() => {
    const node = nodeRef.current;
    if (node) {
      detach.current?.();
      detach.current = attach(node);
    }
    return () => {
      detach.current?.();
      detach.current = null;
    };
  }, [attach]);

  return { ref };
}
