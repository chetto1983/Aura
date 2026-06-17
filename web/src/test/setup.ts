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

afterEach(() => {
  cleanup();
});
