import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useEdgeSwipe } from '../useEdgeSwipe';

// A non-passive, capture-phase touch listener attached at the shell root is the only
// way preventDefault() can claim the gesture from the browser back-swipe/overscroll.
// We exercise the hook by mounting it (renderHook), attaching its returned ref to a
// real DOM element, and dispatching genuine TouchEvents with cancelable:true so we can
// observe preventDefault() / defaultPrevented exactly as the browser would.

function setWidth(width: number): void {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
}

interface Pt {
  readonly x: number;
  readonly y: number;
}

function touchEvent(type: string, points: readonly Pt[], target: EventTarget): TouchEvent {
  const touches = points.map((p) => ({ clientX: p.x, clientY: p.y, target }) as unknown as Touch);
  // jsdom has no TouchEvent constructor; build a CustomEvent and graft touch lists on.
  const event = new Event(type, { bubbles: true, cancelable: true }) as unknown as TouchEvent & {
    touches: Touch[];
    changedTouches: Touch[];
  };
  Object.defineProperty(event, 'touches', { value: touches, configurable: true });
  Object.defineProperty(event, 'changedTouches', { value: touches, configurable: true });
  Object.defineProperty(event, 'target', { value: target, configurable: true });
  return event;
}

function mountOn(
  el: HTMLElement,
  options: Parameters<typeof useEdgeSwipe>[0],
): { unmount: () => void } {
  const { result, unmount } = renderHook(() => useEdgeSwipe(options));
  result.current.ref(el);
  return { unmount };
}

describe('useEdgeSwipe — §3.1b gesture contract', () => {
  let host: HTMLElement;

  beforeEach(() => {
    setWidth(400);
    host = document.createElement('div');
    document.body.appendChild(host);
  });

  afterEach(() => {
    host.remove();
    vi.restoreAllMocks();
  });

  it('returns a ref callback (spreadable onto the shell element)', () => {
    const { result } = renderHook(() => useEdgeSwipe({ onLeftEdge: vi.fn() }));
    expect(typeof result.current.ref).toBe('function');
  });

  it('arms only inside the left edge zone, locks horizontal after 10px, and opens at >50%', () => {
    const onLeftEdge = vi.fn();
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

    const child = document.createElement('span');
    host.appendChild(child);

    host.dispatchEvent(touchEvent('touchstart', [{ x: 8, y: 100 }], child));
    // <10px travel: no lock yet, callback not fired.
    host.dispatchEvent(touchEvent('touchmove', [{ x: 14, y: 102 }], child));
    expect(onLeftEdge).not.toHaveBeenCalled();
    // past 10px and horizontal — locks; drag well past 50% of the panel width.
    host.dispatchEvent(touchEvent('touchmove', [{ x: 220, y: 104 }], child));
    host.dispatchEvent(touchEvent('touchend', [{ x: 220, y: 104 }], child));
    expect(onLeftEdge).toHaveBeenCalledTimes(1);

    unmount();
  });

  it('does NOT arm when the touch starts outside the edge zone', () => {
    const onLeftEdge = vi.fn();
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

    host.dispatchEvent(touchEvent('touchstart', [{ x: 120, y: 100 }], host));
    host.dispatchEvent(touchEvent('touchmove', [{ x: 320, y: 102 }], host));
    host.dispatchEvent(touchEvent('touchend', [{ x: 320, y: 102 }], host));
    expect(onLeftEdge).not.toHaveBeenCalled();

    unmount();
  });

  it('calls preventDefault() once the gesture locks horizontal (claims the back-swipe)', () => {
    const { unmount } = mountOn(host, { onLeftEdge: vi.fn(), edgeSize: 20 });

    host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], host));
    const move = touchEvent('touchmove', [{ x: 60, y: 104 }], host);
    host.dispatchEvent(move);
    expect(move.defaultPrevented).toBe(true);

    unmount();
  });

  it('does NOT preventDefault a vertical gesture (lets the chat/list scroll)', () => {
    const onLeftEdge = vi.fn();
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

    host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], host));
    const move = touchEvent('touchmove', [{ x: 9, y: 180 }], host); // |dy| > |dx|
    host.dispatchEvent(move);
    host.dispatchEvent(touchEvent('touchend', [{ x: 9, y: 220 }], host));
    expect(move.defaultPrevented).toBe(false);
    expect(onLeftEdge).not.toHaveBeenCalled();

    unmount();
  });

  it('aborts an armed horizontal drag when vertical drift later exceeds ~40px', () => {
    const onLeftEdge = vi.fn();
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

    host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], host));
    host.dispatchEvent(touchEvent('touchmove', [{ x: 60, y: 102 }], host)); // locks horizontal
    host.dispatchEvent(touchEvent('touchmove', [{ x: 120, y: 150 }], host)); // dy 50px > 40 abort
    host.dispatchEvent(touchEvent('touchend', [{ x: 240, y: 152 }], host));
    expect(onLeftEdge).not.toHaveBeenCalled();

    unmount();
  });

  it('does not commit a short horizontal drag below 50% (springs back)', () => {
    vi.useFakeTimers();
    const onLeftEdge = vi.fn();
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

    vi.setSystemTime(0);
    host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], host));
    vi.setSystemTime(20); // a quick first sample is still only a short drag, not a fling
    host.dispatchEvent(touchEvent('touchmove', [{ x: 40, y: 102 }], host)); // locked, but small
    host.dispatchEvent(touchEvent('touchend', [{ x: 40, y: 102 }], host));
    expect(onLeftEdge).not.toHaveBeenCalled();

    vi.useRealTimers();
    unmount();
  });

  it('commits a short-but-flung horizontal drag (velocity rule)', () => {
    vi.useFakeTimers();
    const onLeftEdge = vi.fn();
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

    vi.setSystemTime(0);
    host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], host));
    host.dispatchEvent(touchEvent('touchmove', [{ x: 40, y: 102 }], host));
    vi.setSystemTime(20); // 20ms later, big jump → high velocity, still < 50%
    host.dispatchEvent(touchEvent('touchmove', [{ x: 120, y: 103 }], host));
    host.dispatchEvent(touchEvent('touchend', [{ x: 120, y: 103 }], host));
    expect(onLeftEdge).toHaveBeenCalledTimes(1);

    vi.useRealTimers();
    unmount();
  });

  it('opens the right runtime sheet on a right-edge right→left drag', () => {
    const onRightEdge = vi.fn();
    const { unmount } = mountOn(host, { onRightEdge, edgeSize: 20 });

    host.dispatchEvent(touchEvent('touchstart', [{ x: 396, y: 100 }], host));
    host.dispatchEvent(touchEvent('touchmove', [{ x: 380, y: 102 }], host)); // lock
    host.dispatchEvent(touchEvent('touchmove', [{ x: 150, y: 104 }], host)); // past 50%
    host.dispatchEvent(touchEvent('touchend', [{ x: 150, y: 104 }], host));
    expect(onRightEdge).toHaveBeenCalledTimes(1);

    unmount();
  });

  it.each(['pre', 'code', 'table', 'input', 'textarea'])(
    'does not arm when the touch starts inside <%s> (scroll-owner exclude list)',
    (tag) => {
      const onLeftEdge = vi.fn();
      const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

      const excluded = document.createElement(tag);
      host.appendChild(excluded);
      host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], excluded));
      host.dispatchEvent(touchEvent('touchmove', [{ x: 220, y: 104 }], excluded));
      host.dispatchEvent(touchEvent('touchend', [{ x: 220, y: 104 }], excluded));
      expect(onLeftEdge).not.toHaveBeenCalled();

      unmount();
    },
  );

  it.each(['contenteditable', 'data-no-swipe', 'data-drawer'])(
    'does not arm when the touch starts inside [%s]',
    (attr) => {
      const onLeftEdge = vi.fn();
      const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

      const excluded = document.createElement('div');
      excluded.setAttribute(attr, '');
      host.appendChild(excluded);
      const inner = document.createElement('span');
      excluded.appendChild(inner);
      host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], inner));
      host.dispatchEvent(touchEvent('touchmove', [{ x: 220, y: 104 }], inner));
      host.dispatchEvent(touchEvent('touchend', [{ x: 220, y: 104 }], inner));
      expect(onLeftEdge).not.toHaveBeenCalled();

      unmount();
    },
  );

  it('does not arm when the touch starts inside [role="dialog"]', () => {
    const onLeftEdge = vi.fn();
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });

    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    host.appendChild(dialog);
    host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], dialog));
    host.dispatchEvent(touchEvent('touchmove', [{ x: 220, y: 104 }], dialog));
    host.dispatchEvent(touchEvent('touchend', [{ x: 220, y: 104 }], dialog));
    expect(onLeftEdge).not.toHaveBeenCalled();

    unmount();
  });

  it('removes its listeners on unmount (no leak, no callback after teardown)', () => {
    const onLeftEdge = vi.fn();
    const removeSpy = vi.spyOn(host, 'removeEventListener');
    const { unmount } = mountOn(host, { onLeftEdge, edgeSize: 20 });
    unmount();

    expect(removeSpy).toHaveBeenCalled();
    host.dispatchEvent(touchEvent('touchstart', [{ x: 6, y: 100 }], host));
    host.dispatchEvent(touchEvent('touchmove', [{ x: 220, y: 104 }], host));
    host.dispatchEvent(touchEvent('touchend', [{ x: 220, y: 104 }], host));
    expect(onLeftEdge).not.toHaveBeenCalled();
  });

  it('attaches its move/end listeners in the capture phase, non-passive', () => {
    const addSpy = vi.spyOn(host, 'addEventListener');
    const { unmount } = mountOn(host, { onLeftEdge: vi.fn() });

    const move = addSpy.mock.calls.find((c) => c[0] === 'touchmove');
    expect(move).toBeTruthy();
    const opts = move?.[2] as AddEventListenerOptions;
    expect(opts.capture).toBe(true);
    expect(opts.passive).toBe(false);

    unmount();
  });

  it('closes the left drawer on a leftward swipe that starts inside an open panel', () => {
    const onLeftClose = vi.fn();
    const { unmount } = mountOn(host, {
      onLeftEdge: vi.fn(),
      onLeftClose,
      isLeftOpen: true,
      edgeSize: 20,
    });

    // Start anywhere (panel is open), drag left→ closed past 50%.
    host.dispatchEvent(touchEvent('touchstart', [{ x: 200, y: 100 }], host));
    host.dispatchEvent(touchEvent('touchmove', [{ x: 150, y: 102 }], host)); // lock horizontal
    host.dispatchEvent(touchEvent('touchmove', [{ x: 10, y: 104 }], host)); // past 50% leftward
    host.dispatchEvent(touchEvent('touchend', [{ x: 10, y: 104 }], host));
    expect(onLeftClose).toHaveBeenCalledTimes(1);

    unmount();
  });
});
