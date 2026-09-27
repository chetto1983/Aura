import { useCallback, useEffect, useRef, useState } from 'react';
import { parseRelayLine, type FrameMeta } from './liveInput';

export type LivePhase = 'connecting' | 'live' | 'ended';

export interface LiveState {
  phase: LivePhase;
  frame: string | null;
  meta: FrameMeta | null;
  url: string;
  /** Why the view ended: the relay's own reason, `taken_over`, or `disconnected`. */
  reason: string;
}

const initial: LiveState = { phase: 'connecting', frame: null, meta: null, url: '', reason: '' };

function sessionPath(session: string): string {
  return `/api/browser/sessions/${encodeURIComponent(session)}`;
}

/**
 * Watches one agent-browser session of the caller's own box and returns a sender for viewer
 * input. The server keys the viewer on identity + session, so this page can only ever drive the
 * signed-in user's browser; opening a second viewer ends this one.
 */
export function useBrowserLive(session: string): {
  state: LiveState;
  send: (event: object) => void;
} {
  // Mount it under key={session}: state starts fresh per session without a reset in an effect.
  const [state, setState] = useState<LiveState>(() =>
    typeof EventSource === 'function'
      ? initial
      : { ...initial, phase: 'ended', reason: 'unsupported' },
  );

  useEffect(() => {
    if (typeof EventSource !== 'function') return undefined;
    const source = new EventSource(`${sessionPath(session)}/stream`);
    const onMessage = (event: MessageEvent) => {
      if (typeof event.data !== 'string') return;
      const msg = parseRelayLine(event.data);
      if (msg.kind === 'frame')
        setState((s) => ({ ...s, phase: 'live', frame: msg.src, meta: msg.meta }));
      else if (msg.kind === 'url') setState((s) => ({ ...s, url: msg.url }));
      else if (msg.kind === 'error') {
        source.close();
        setState((s) => ({ ...s, phase: 'ended', reason: msg.reason }));
      }
    };
    const onError = () => {
      source.close();
      setState((s) => (s.phase === 'ended' ? s : { ...s, phase: 'ended', reason: 'disconnected' }));
    };
    source.addEventListener('message', onMessage);
    source.addEventListener('error', onError);
    return () => {
      source.removeEventListener('message', onMessage);
      source.removeEventListener('error', onError);
      source.close();
    };
  }, [session]);

  // One POST at a time: two in flight on separate connections can land out of order, and
  // keystrokes that swap places type the wrong word.
  const queue = useRef<Promise<unknown>>(Promise.resolve());
  const send = useCallback(
    (event: object) => {
      queue.current = queue.current.then(() =>
        fetch(`${sessionPath(session)}/input`, {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(event),
        }).then(
          (res) => {
            if (res.status === 409)
              setState((s) => ({ ...s, phase: 'ended', reason: 'taken_over' }));
          },
          () => undefined,
        ),
      );
    },
    [session],
  );

  return { state, send };
}
