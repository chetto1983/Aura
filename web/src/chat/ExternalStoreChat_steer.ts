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
//
// The composer clears on submit, so a steer that cannot be delivered must not take its text with
// it (task #30, measured 2026-10-05: every steer that reached its run after the run ended was
// dropped, and so was every one sent before this tab knew its run's id). A run that ended sends
// the text as the next turn, as the steering design's §4.2 has the client do. Any other refusal,
// and a run with no id yet, hands the text back to the composer.

// The steer sources Aura generates itself (internal/steer IsRuntimeSource): a worker report, a
// background shell exit, a detached video outcome, a backgrounded tool call's result. The
// operator redirected nothing, so none of them earns a "redirected" notice.
const RUNTIME_STEER_SOURCES: ReadonlySet<string> = new Set(['swarm', 'shell', 'media', 'tool']);

export interface SteerNoticeView {
  readonly id: string;
  readonly kind: 'redirected' | 'autoDelivered';
}

/** A refused steer: the message to show, and the text it carried, for the composer. */
export interface SteerRefusalView {
  readonly id: string;
  readonly message: string;
  readonly draft: string;
}

export interface UseSteerSendArgs {
  readonly threadId: string;
  /** The conversation DTO's live_run_id — the reattached-tab fallback. */
  readonly liveRunId: string | undefined;
  /** The run THIS tab is driving/attached to — preferred over liveRunId (D-10 §Step 2). */
  readonly activeRunIdRef: RefObject<string | null>;
  /** Whether this tab is running a stream now, read when a steer is sent rather than when the
   *  callback was made: the runtime can call an onNew from an earlier render. */
  readonly isRunningRef: RefObject<boolean>;
  readonly setMessages: Dispatch<SetStateAction<ThreadMessageLike[]>>;
}

export interface UseSteerSendResult {
  /** Routes `text` as a steer when a live run exists for this thread. True when the steer was
   *  handled here: accepted, or refused with its text returned. False ⇒ the caller must send it
   *  as an ordinary turn: there is no live run, or the run ended before the steer reached it. */
  readonly trySend: (text: string) => Promise<boolean>;
  /** Wire into streamRun/streamPost/attachRun's onSteer option on every open pump. */
  readonly onFrame: (frame: SteerFramePayload) => void;
  readonly notice: SteerNoticeView | undefined;
  readonly refusal: SteerRefusalView | undefined;
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
  isRunningRef,
  setMessages,
}: UseSteerSendArgs): UseSteerSendResult {
  const { t } = useTranslation();
  const [notice, setNotice] = useState<SteerNoticeView | undefined>(undefined);
  const [refusal, setRefusal] = useState<SteerRefusalView | undefined>(undefined);
  const seenIdsRef = useRef<Set<string>>(new Set());
  const pendingTextRef = useRef<string | undefined>(undefined);
  // Runs the server said had ended. The ids this tab holds can outlive the run by a stream's
  // last frames or a cached conversation read, and a second steer at one is a second 410.
  const endedRunIdsRef = useRef<Set<string>>(new Set());

  const resolveRunId = useCallback((): string | undefined => {
    return [activeRunIdRef.current, liveRunId].find(
      (id): id is string =>
        id !== null && id !== undefined && id.length > 0 && !endedRunIdsRef.current.has(id),
    );
  }, [activeRunIdRef, liveRunId]);

  const onFrame = useCallback(
    (frame: SteerFramePayload) => {
      if (frame.conversation_id !== threadId) return;
      for (const entry of frame.steers) {
        if (RUNTIME_STEER_SOURCES.has(entry.source)) continue;
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
      if (runId === undefined) {
        if (!isRunningRef.current || activeRunIdRef.current !== null) return false;
        // This tab's run has no id to steer: it has not reported one yet, or it is a resume or a
        // re-run, which never do. A new turn would collide with it (409) and be overwritten.
        setRefusal({
          id: crypto.randomUUID(),
          message: t('chat.steer.refusal.notYet'),
          draft: text,
        });
        return true;
      }
      setRefusal(undefined);
      const optimistic = userMessage(text);
      setMessages((prev) => [...prev, optimistic]);
      pendingTextRef.current = text;
      try {
        await steerRun(runId, text).send();
        setNotice({ id: `local-${crypto.randomUUID()}`, kind: 'redirected' });
        return true;
      } catch (err) {
        pendingTextRef.current = undefined;
        setMessages((prev) => prev.filter((m) => m.id !== optimistic.id));
        if (err instanceof SteerRefusal && err.kind === 'ended') {
          endedRunIdsRef.current.add(runId);
          setNotice({ id: `local-${crypto.randomUUID()}`, kind: 'autoDelivered' });
          return false;
        }
        setRefusal({ id: crypto.randomUUID(), message: t(refusalKey(err)), draft: text });
        return true;
      }
    },
    [activeRunIdRef, isRunningRef, resolveRunId, setMessages, t],
  );

  const dismissNotice = useCallback(() => {
    setNotice(undefined);
  }, []);

  return { trySend, onFrame, notice, refusal, dismissNotice };
}
