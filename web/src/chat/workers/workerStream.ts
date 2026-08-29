import type { ThreadMessageLike } from '@assistant-ui/react';
import { reduceFrame } from '../sseAdapter';
import type { AguiFrame } from '../sseAdapter_frames';
import { newAssistantTurn } from '../sseAdapter_parts';

export interface WorkerStreamHandlers {
  readonly onMessages: (messages: readonly ThreadMessageLike[]) => void;
  readonly onError: () => void;
}

export interface WorkerStreamSubscription {
  readonly close: () => void;
}

function workerEventsURL(conversationId: string, childId?: string): string {
  const base = `/api/conversations/${encodeURIComponent(conversationId)}/swarm/events`;
  return childId === undefined ? base : `${base}?child=${encodeURIComponent(childId)}`;
}

/** Replay and tail one worker through the same AG-UI reducer used by the parent thread. */
export function openWorkerStream(
  conversationId: string,
  childId: string,
  handlers: WorkerStreamHandlers,
): WorkerStreamSubscription {
  const source = new EventSource(workerEventsURL(conversationId, childId));
  const state = newAssistantTurn(childId);

  const onMessage: EventListener = (event) => {
    if (!(event instanceof MessageEvent) || typeof event.data !== 'string') return;
    try {
      const frame = JSON.parse(event.data) as AguiFrame;
      reduceFrame(state, frame);
      handlers.onMessages([
        {
          id: childId,
          role: 'assistant',
          content: [...state.content],
          status: state.status,
        },
      ]);
    } catch {
      handlers.onError();
    }
  };
  const onError: EventListener = () => {
    handlers.onError();
  };

  source.addEventListener('message', onMessage);
  source.addEventListener('error', onError);

  return {
    close: () => {
      source.removeEventListener('message', onMessage);
      source.removeEventListener('error', onError);
      source.close();
    },
  };
}

export { workerEventsURL };
