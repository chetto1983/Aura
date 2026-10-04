import { act, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from 'vitest';
import type { ThreadOpenAt } from '../ExternalStoreChat_props';
import { ThreadViewport } from '../ExternalStoreChat_viewport';

// ThreadViewport is assistant-ui's Viewport with the bottom pinning held off while a search hit
// is on screen. The double here exposes the three pinning props as attributes and captures the
// runStart listener, so the test can read what the library was told and start a run.

const H = vi.hoisted((): { runStart: (() => void) | undefined } => ({ runStart: undefined }));

interface ViewportProps {
  readonly className?: string;
  readonly autoScroll?: boolean;
  readonly scrollToBottomOnInitialize?: boolean;
  readonly scrollToBottomOnThreadSwitch?: boolean;
  readonly children?: ReactNode;
}

vi.mock('@assistant-ui/react', async () => {
  const React = await import('react');
  return {
    ThreadPrimitive: {
      Viewport: React.forwardRef<HTMLDivElement, ViewportProps>(function Viewport(props, ref) {
        return (
          <div
            ref={ref}
            data-testid="viewport"
            data-auto-scroll={String(props.autoScroll)}
            data-initialize={String(props.scrollToBottomOnInitialize)}
            data-thread-switch={String(props.scrollToBottomOnThreadSwitch)}
          >
            {props.children}
          </div>
        );
      }),
    },
    useAuiEvent: (event: string, callback: () => void) => {
      if (event === 'thread.runStart') H.runStart = callback;
    },
  };
});

function persisted(seq: number): ThreadMessageLike {
  return {
    id: `msg-${String(seq)}`,
    role: seq % 2 === 1 ? 'user' : 'assistant',
    content: [{ type: 'text', text: `turn ${String(seq)}` }],
    metadata: { custom: { backendSeq: seq } },
  };
}

const MESSAGES = [1, 2, 3, 4].map(persisted);

function hit(seq: number, nonce = 1): ThreadOpenAt {
  return { threadId: 't', seq, nonce };
}

function viewport(
  openAt: ThreadOpenAt | undefined,
  {
    historyReady = true,
    drawn = MESSAGES,
  }: { historyReady?: boolean; drawn?: readonly ThreadMessageLike[] } = {},
) {
  return (
    <ThreadViewport className="" openAt={openAt} messages={MESSAGES} historyReady={historyReady}>
      {drawn.map((message) => (
        <div key={message.id} data-message-id={message.id} />
      ))}
    </ThreadViewport>
  );
}

function message(seq: number): HTMLElement {
  const element = document.querySelector<HTMLElement>(`[data-message-id="msg-${String(seq)}"]`);
  if (element === null) throw new Error(`no msg-${String(seq)}`);
  return element;
}

function pinning(): readonly (string | undefined)[] {
  const element = screen.getByTestId('viewport');
  return [element.dataset.autoScroll, element.dataset.initialize, element.dataset.threadSwitch];
}

function nextFrames(count: number) {
  act(() => {
    for (let i = 0; i < count; i++) vi.advanceTimersToNextFrame();
  });
}

function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

function startRun() {
  act(() => {
    H.runStart?.();
  });
}

const HELD = ['false', 'false', 'false'];
const DEFAULT = ['true', 'true', 'true'];

let scrollIntoView: MockInstance<Element['scrollIntoView']>;
let scrollTo: MockInstance<(options?: ScrollToOptions) => void>;

beforeEach(() => {
  vi.useFakeTimers({
    toFake: ['setTimeout', 'clearTimeout', 'requestAnimationFrame', 'cancelAnimationFrame'],
  });
  scrollIntoView = vi.spyOn(Element.prototype, 'scrollIntoView');
  scrollTo = vi.spyOn(Element.prototype, 'scrollTo');
  H.runStart = undefined;
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('ThreadViewport', () => {
  it('leaves the bottom pinning on when no search hit opened the thread', () => {
    render(viewport(undefined));
    nextFrames(2);
    expect(pinning()).toEqual(DEFAULT);
    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(scrollTo).not.toHaveBeenCalled();
  });

  it('brings the matched message to the middle and marks it, with the pinning held off', () => {
    render(viewport(hit(3)));
    expect(pinning()).toEqual(HELD);
    nextFrames(1);
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
    expect(scrollIntoView.mock.contexts[0]).toBe(message(3));
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'center' });
    expect(message(3).getAttribute('data-search-hit')).toBe('');
    expect(message(2).hasAttribute('data-search-hit')).toBe(false);
  });

  // The runtime draws the messages a commit after they reach it.
  it('waits for the matched message to be drawn', () => {
    const { rerender } = render(viewport(hit(3), { drawn: [] }));
    nextFrames(3);
    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(scrollTo).not.toHaveBeenCalled();
    rerender(viewport(hit(3)));
    nextFrames(1);
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
    expect(scrollIntoView.mock.contexts[0]).toBe(message(3));
  });

  it('opens at the bottom when the matched message is never drawn', () => {
    render(viewport(hit(3), { drawn: [] }));
    Object.defineProperty(screen.getByTestId('viewport'), 'scrollHeight', { value: 777 });
    nextFrames(30);
    expect(scrollTo).not.toHaveBeenCalled();
    nextFrames(1);
    expect(scrollTo).toHaveBeenCalledWith({ top: 777 });
    expect(scrollIntoView).not.toHaveBeenCalled();
  });

  // A worker report or a tool result folded into its card has no message of its own.
  it('opens at the bottom at once when no visible message carries the seq', () => {
    render(viewport(hit(9)));
    expect(pinning()).toEqual(DEFAULT);
    Object.defineProperty(screen.getByTestId('viewport'), 'scrollHeight', { value: 1_234 });
    nextFrames(1);
    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(scrollTo).toHaveBeenCalledTimes(1);
    expect(scrollTo.mock.contexts[0]).toBe(screen.getByTestId('viewport'));
    expect(scrollTo).toHaveBeenCalledWith({ top: 1_234 });
    // The unmark timer still fires, with nothing marked.
    advance(3_000);
  });

  it('removes the mark after a while', () => {
    render(viewport(hit(3)));
    nextFrames(1);
    advance(2_300);
    expect(message(3).hasAttribute('data-search-hit')).toBe(true);
    advance(200);
    expect(message(3).hasAttribute('data-search-hit')).toBe(false);
  });

  // A second click inside the mark's lifetime must not lose its own mark to the first timer.
  it('keeps the mark of a second click for its full time', () => {
    const { rerender } = render(viewport(hit(3, 1)));
    nextFrames(1);
    advance(1_000);
    rerender(viewport(hit(3, 2)));
    nextFrames(1);
    advance(1_500);
    expect(message(3).hasAttribute('data-search-hit')).toBe(true);
    advance(1_000);
    expect(message(3).hasAttribute('data-search-hit')).toBe(false);
  });

  it('opens a message taller than the viewport at its top', () => {
    render(viewport(hit(3)));
    Object.defineProperty(message(3), 'offsetHeight', { value: 900 });
    Object.defineProperty(screen.getByTestId('viewport'), 'clientHeight', { value: 400 });
    nextFrames(1);
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'start' });
  });

  it('holds the pinning off but does not scroll before the history has loaded', () => {
    render(viewport(hit(3), { historyReady: false }));
    nextFrames(2);
    expect(pinning()).toEqual(HELD);
    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(scrollTo).not.toHaveBeenCalled();
  });

  // assistant-ui can still owe a jump to the bottom from an earlier thread; only a fresh
  // viewport is free of it.
  it('mounts a fresh viewport for every hit, and keeps it when a run releases the hit', () => {
    const { rerender } = render(viewport(undefined));
    const before = screen.getByTestId('viewport');
    rerender(viewport(hit(3, 1)));
    const first = screen.getByTestId('viewport');
    expect(first).not.toBe(before);
    nextFrames(1);
    startRun();
    expect(screen.getByTestId('viewport')).toBe(first);
    rerender(viewport(hit(3, 2)));
    expect(screen.getByTestId('viewport')).not.toBe(first);
  });

  it('hands the viewport back when a run starts, and unmarks the message', () => {
    render(viewport(hit(3)));
    nextFrames(1);
    startRun();
    expect(pinning()).toEqual(DEFAULT);
    expect(message(3).hasAttribute('data-search-hit')).toBe(false);
  });

  it('does not scroll when a run starts before the next frame', () => {
    render(viewport(hit(3)));
    startRun();
    nextFrames(2);
    expect(scrollIntoView).not.toHaveBeenCalled();
  });

  it('keeps the default once the released thread is left', () => {
    const { rerender } = render(viewport(hit(3)));
    nextFrames(1);
    startRun();
    rerender(viewport(undefined));
    expect(pinning()).toEqual(DEFAULT);
  });

  it('opens the same turn again on a new click after a run released it', () => {
    const { rerender } = render(viewport(hit(3, 1)));
    nextFrames(1);
    startRun();
    rerender(viewport(hit(3, 2)));
    expect(pinning()).toEqual(HELD);
    nextFrames(1);
    expect(scrollIntoView).toHaveBeenCalledTimes(2);
  });
});
