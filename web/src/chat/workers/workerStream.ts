import type { ThreadMessageLike } from '@assistant-ui/react';
import { reduceFrame } from '../sseAdapter';
import type { AguiFrame } from '../sseAdapter_frames';
import { newAssistantTurn } from '../sseAdapter_parts';
import { isSwarmStatus, type SwarmStatus } from '../displays/swarmRow';

export interface WorkerStreamHandlers {
  readonly onMessages: (messages: readonly ThreadMessageLike[]) => void;
  readonly onError: () => void;
}

export interface WorkerStreamSubscription {
  readonly close: () => void;
}

export interface WorkerStatus {
  readonly child_id: string;
  readonly status: SwarmStatus;
  readonly last_event_at: string;
  readonly events: number;
  readonly duration_sec: number;
}

export interface WorkerStatusStreamHandlers {
  readonly onStatus: (status: WorkerStatus) => void;
  readonly onError: () => void;
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

function decodeWorkerStatus(value: unknown): WorkerStatus | null {
  if (typeof value !== 'object' || value === null) return null;
  const candidate = value as Record<string, unknown>;
  if (
    typeof candidate.child_id !== 'string' ||
    typeof candidate.status !== 'string' ||
    !isSwarmStatus(candidate.status) ||
    typeof candidate.last_event_at !== 'string' ||
    typeof candidate.events !== 'number' ||
    typeof candidate.duration_sec !== 'number'
  ) {
    return null;
  }
  return {
    child_id: candidate.child_id,
    status: candidate.status,
    last_event_at: candidate.last_event_at,
    events: candidate.events,
    duration_sec: candidate.duration_sec,
  };
}

/** Subscribe once to the conversation-wide bounded worker-status stream. */
export function openWorkerStatusStream(
  conversationId: string,
  handlers: WorkerStatusStreamHandlers,
): WorkerStreamSubscription {
  const source = new EventSource(workerEventsURL(conversationId));
  const onMessage: EventListener = (event) => {
    if (!(event instanceof MessageEvent) || typeof event.data !== 'string') return;
    try {
      const frame = JSON.parse(event.data) as AguiFrame;
      if (frame.type !== 'CUSTOM' || frame.name !== 'aura.swarm.worker') return;
      const status = decodeWorkerStatus(frame.value);
      if (status !== null) handlers.onStatus(status);
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

export { decodeWorkerStatus, workerEventsURL };
