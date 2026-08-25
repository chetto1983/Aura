import {
  useCallback,
  useRef,
  useState,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from 'react';
import { useTranslation } from 'react-i18next';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { userMessage } from './ExternalStoreChat_folds';
import { steerRun, SteerRefusal } from './steerRun';
import type { SteerNotice as SteerFramePayload } from './sseAdapter';

// ExternalStoreChat_steer — D-10's composer contract (the half of it nothing owned before this
// plan) split out of ExternalStoreChat.tsx (600-LOC cap, the ExternalStoreChat_liveRun.ts
// precedent). Owns: the decision that a live-run submit is a steer, the optimistic user append
// and its rollback on refusal, the steerRun call, and the notice state fed by BOTH this tab's
// own send and the aura.steer pump signal (onFrame) — de-duplicated so a tab that both sends
// and observes shows exactly one notice, matching the behaviour the plan's acceptance criteria
// pin down.

export interface SteerNoticeView {
  readonly id: string;
  readonly kind: 'redirected' | 'autoDelivered';
}

export interface UseSteerSendArgs {
  readonly threadId: string;
  /** The conversation DTO's live_run_id — the reattached-tab fallback. */
  readonly liveRunId: string | undefined;
  /** The run THIS tab is driving/attached to — preferred over liveRunId (D-10 §Step 2). */
  readonly activeRunIdRef: RefObject<string | null>;
  /** The component's own running state — the render-time steering gate. `trySend` resolves the
   *  ACTUAL run id independently (event-handler context, not render — react-hooks/refs forbids
   *  reading activeRunIdRef.current during render, which is why `available` is NOT derived from
   *  it: isRunning tracks it closely enough in practice, and trySend's own resolution is what
   *  actually has to be correct). */
  readonly isRunning: boolean;
  readonly setMessages: Dispatch<SetStateAction<ThreadMessageLike[]>>;
}

export interface UseSteerSendResult {
  /** True while a steerable run exists for this thread — Composer's dedicated-control gate. */
  readonly available: boolean;
  /** Routes `text` as a steer when a live run exists for this thread; false ⇒ the caller must
   *  send it normally (no live run). Never both: a handled steer (success OR refusal) always
   *  returns true so the caller does not ALSO POST /agent/run for the same text. */
  readonly trySend: (text: string) => Promise<boolean>;
  /** Wire into streamRun/streamPost/attachRun's onSteer option on every open pump. */
  readonly onFrame: (frame: SteerFramePayload) => void;
  readonly notice: SteerNoticeView | undefined;
  readonly refusalText: string | undefined;
  readonly dismissNotice: () => void;
}

function refusalKey(err: unknown): string {
  if (err instanceof SteerRefusal) return `chat.steer.refusal.${err.kind}`;
  return 'chat.steer.refusal.failed';
}

export function useSteerSend({
  threadId,
  liveRunId,
  activeRunIdRef,
  isRunning,
  setMessages,
}: UseSteerSendArgs): UseSteerSendResult {
  const { t } = useTranslation();
  const [notice, setNotice] = useState<SteerNoticeView | undefined>(undefined);
  const [refusalText, setRefusalText] = useState<string | undefined>(undefined);
  const seenIdsRef = useRef<Set<string>>(new Set());
  const pendingTextRef = useRef<string | undefined>(undefined);

  const resolveRunId = useCallback((): string | undefined => {
    const active = activeRunIdRef.current;
    if (active !== null && active.length > 0) return active;
    return liveRunId !== undefined && liveRunId.length > 0 ? liveRunId : undefined;
  }, [activeRunIdRef, liveRunId]);

  const available = isRunning;

  const onFrame = useCallback(
    (frame: SteerFramePayload) => {
      if (frame.conversation_id !== threadId) return;
      for (const entry of frame.steers) {
        if (seenIdsRef.current.has(entry.id)) continue;
        seenIdsRef.current.add(entry.id);
        if (entry.source === 'cockpit' && entry.text === pendingTextRef.current) {
          // The confirmation of this tab's own send — already shown from trySend below.
          pendingTextRef.current = undefined;
          continue;
        }
        setNotice({
          id: entry.id,
          kind: entry.delivery === 'auto_delivery_next_turn' ? 'autoDelivered' : 'redirected',
        });
      }
    },
    [threadId],
  );

  const trySend = useCallback(
    async (text: string): Promise<boolean> => {
      const runId = resolveRunId();
      if (runId === undefined) return false;
      setRefusalText(undefined);
      const optimistic = userMessage(text);
      setMessages((prev) => [...prev, optimistic]);
      pendingTextRef.current = text;
      try {
        await steerRun(runId, text).send();
        setNotice({ id: `local-${crypto.randomUUID()}`, kind: 'redirected' });
      } catch (err) {
        pendingTextRef.current = undefined;
        setMessages((prev) => prev.filter((m) => m.id !== optimistic.id));
        setRefusalText(t(refusalKey(err)));
      }
      return true; // handled either way — the caller must not also send /agent/run
    },
    [resolveRunId, setMessages, t],
  );

  const dismissNotice = useCallback(() => {
    setNotice(undefined);
  }, []);

  return { available, trySend, onFrame, notice, refusalText, dismissNotice };
}
