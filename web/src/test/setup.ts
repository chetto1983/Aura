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

afterEach(() => {
  cleanup();
});
