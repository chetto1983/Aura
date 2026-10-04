import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ThreadPrimitive, useAuiEvent, type ThreadMessageLike } from '@assistant-ui/react';
import type { ThreadOpenAt } from './ExternalStoreChat_props';
import { messageIdAtSeq } from './messageSeq';

/** How long the message a search hit opened stays marked ([data-search-hit], motion.css). */
const SEARCH_HIT_MARK_MS = 2400;
/** How many frames to wait for the matched message to reach the DOM (about half a second). */
const FIND_FRAMES = 30;

interface ThreadViewportProps {
  readonly className: string;
  readonly openAt: ThreadOpenAt | undefined;
  readonly messages: readonly ThreadMessageLike[];
  /** This thread's history has loaded. */
  readonly historyReady: boolean;
  readonly children: ReactNode;
}

// The thread's scroll container. assistant-ui keeps it at the bottom: it jumps there when the
// history loads and follows every content resize while it is there
// (useThreadViewportAutoScroll, @assistant-ui/react 0.15). A thread opened on a search hit
// turns that off, brings the matched message into view and marks it, then hands the viewport
// back when the next run starts, which jumps to the bottom as every run does.
export function ThreadViewport({
  className,
  openAt,
  messages,
  historyReady,
  children,
}: ThreadViewportProps) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const [released, setReleased] = useState<ThreadOpenAt>();
  useAuiEvent('thread.runStart', () => {
    setReleased(openAt);
  });
  const pending = openAt !== undefined && openAt !== released;
  const targetId = pending && historyReady ? messageIdAtSeq(messages, openAt.seq) : undefined;
  // A hit on a turn the thread does not draw has nothing to hold the view on.
  const holding = pending && (!historyReady || targetId !== undefined);

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!pending || !historyReady || viewport === null) return;
    let target: HTMLElement | undefined;
    let frames = 0;
    // The runtime draws the messages a commit after they reach it, so the matched one is
    // looked for on each frame until it is there.
    const open = () => {
      target = messageElement(viewport, targetId);
      if (target === undefined && targetId !== undefined && frames++ < FIND_FRAMES) {
        frame = requestAnimationFrame(open);
        return;
      }
      if (target === undefined) {
        viewport.scrollTo({ top: viewport.scrollHeight });
        return;
      }
      // Marked first, so the scroll honours the mark's scroll-margin and keeps the ring in
      // view. A message taller than the viewport opens at its top, where reading starts.
      target.setAttribute('data-search-hit', '');
      const block = target.offsetHeight > viewport.clientHeight ? 'start' : 'center';
      target.scrollIntoView({ block });
    };
    let frame = requestAnimationFrame(open);
    const unmark = setTimeout(() => target?.removeAttribute('data-search-hit'), SEARCH_HIT_MARK_MS);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(unmark);
      target?.removeAttribute('data-search-hit');
    };
  }, [pending, historyReady, targetId, openAt]);

  return (
    <ThreadPrimitive.Viewport
      // A new viewport for every hit: assistant-ui can still owe a jump to the bottom from
      // an earlier thread (one that never overflowed never settles it), and it takes that
      // jump on the next content resize whatever autoScroll says. A fresh one owes nothing.
      key={openAt?.nonce ?? 0}
      ref={viewportRef}
      className={className}
      autoScroll={!holding}
      scrollToBottomOnInitialize={!holding}
      scrollToBottomOnThreadSwitch={!holding}
    >
      {children}
    </ThreadPrimitive.Viewport>
  );
}

function messageElement(viewport: HTMLElement, id: string | undefined): HTMLElement | undefined {
  return Array.from(viewport.querySelectorAll<HTMLElement>('[data-message-id]')).find(
    (element) => element.dataset.messageId === id,
  );
}
