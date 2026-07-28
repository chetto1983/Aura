import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';

// jsdom does not implement ResizeObserver, which assistant-ui's ThreadPrimitive
// viewport (useOnResizeContent) requires. Provide a no-op polyfill so chat
// components mount under jsdom. The real browser supplies the native impl.
if (typeof globalThis.ResizeObserver === 'undefined') {
  const noop = (): void => undefined;
  globalThis.ResizeObserver = class {
    readonly observe = noop;
    readonly unobserve = noop;
    readonly disconnect = noop;
  };
}

// jsdom does not implement Element.scrollTo, which assistant-ui's viewport
// auto-scroll (useThreadViewportAutoScroll) calls on each message. No-op it so
// the chat lane mounts/streams under jsdom without an uncaught exception.
if (typeof Element !== 'undefined' && typeof Element.prototype.scrollTo !== 'function') {
  Element.prototype.scrollTo = (): void => undefined;
}

// jsdom does not implement window.matchMedia, which the governance BoardLayout uses to gate the
// mobile-only bottom-sheet focus trap. Default to a desktop (no-match) media query so components
// mount under jsdom; the real mobile bottom-sheet behaviour is proven by the Playwright e2e.
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  window.matchMedia = (query: string): MediaQueryList =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

// jsdom implements no layout, so prosemirror-view's coordsAtPos — reached when tiptap
// scrolls the selection into view after an editor transaction (SkillBodyEditor, SkillEditor)
// — calls getClientRects()/getBoundingClientRect() on the ranges and elements it measures,
// which jsdom leaves unimplemented, and throws asynchronously. vitest then counts that as an
// unhandled error and fails the run even though every assertion passed. Give both a minimal
// rect so scroll-to-selection is a silent no-op instead of a throw.
if (typeof Range !== 'undefined' && typeof Element !== 'undefined') {
  const rect = (): DOMRect => ({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON: () => ({}),
  });
  const rectList = (): DOMRectList => {
    const rects = [rect()];
    return Object.assign(rects, {
      item: (index: number): DOMRect | null => rects[index] ?? null,
    });
  };
  for (const proto of [Range.prototype, Element.prototype]) {
    proto.getClientRects = rectList;
    proto.getBoundingClientRect = rect;
  }
}

afterEach(() => {
  cleanup();
});
