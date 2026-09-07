import { useCallback, type RefObject } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { assistantErrorMessage, isAbortError } from './ExternalStoreChat_folds';
import { streamPost, type TurnUsage } from './sseAdapter';

// ExternalStoreChat_streams — the scaffolding every non-primary stream shares: cancel the
// history load in flight, take the run lock, open one AbortController, spend one usage
// lifecycle, fold the assistant turn in, surface an error as a turn, and settle.
//
// Split out of ExternalStoreChat.tsx on touch (600-LOC cap, the _folds/_assets/_branches/
// _liveRun precedent). The three callers — a re-run from a branch point, the HITL resume, and
// the RS-07 live-run attach — differ only in WHICH message the update replaces; everything
// around that is identical, and it stayed identical only because it was written once.

export interface StreamFoldDeps {
  readonly threadId: string;
  /** Bumped to invalidate an in-flight history load, whose response must not overwrite the
   * turn this stream is about to produce. */
  readonly historyRequestRef: RefObject<number>;
  readonly historyAbortRef: RefObject<AbortController | null>;
  readonly abortRef: RefObject<AbortController | null>;
  readonly isRunningRef: RefObject<boolean>;
  readonly activeRunIdRef: RefObject<string | null>;
  readonly usageThreadRef: RefObject<string | null>;
  readonly setIsRunning: (running: boolean) => void;
  readonly setMessages: (update: React.SetStateAction<ThreadMessageLike[]>) => void;
  readonly usageLifecycle: {
    readonly start: () => number;
    readonly update: (runId: number, usage: TurnUsage | undefined) => void;
    readonly settle: (runId: number) => void;
  };
  readonly prepareUsageBaseline: (
    threadId: string,
    usageRunId: number,
    signal: AbortSignal,
  ) => Promise<boolean>;
  readonly invalidateRuntimeReads: (id?: string) => void;
  readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
  /** The message shown in place of the assistant turn when the stream fails. */
  readonly streamErrorText: string;
}

export interface StreamFolds {
  /** Re-run from a point: the whole visible list is replaced by `base` + the fresh turn. */
  readonly foldReRun: (url: string, body: unknown, base: ThreadMessageLike[]) => Promise<void>;
  /** Append a fresh assistant turn to the current list, replacing it as the stream grows. */
  readonly foldAppendedStream: (
    runThreadId: string,
    run: (
      controller: AbortController,
      onUpdate: (assistant: ThreadMessageLike, usage: TurnUsage | undefined) => void,
    ) => Promise<unknown>,
  ) => Promise<void>;
  /** The HITL resume: a run with no fresh user message. */
  readonly foldResumeRun: (resumeThreadId: string) => Promise<void>;
}

export function useStreamFolds(deps: StreamFoldDeps): StreamFolds {
  const {
    threadId,
    historyRequestRef,
    historyAbortRef,
    abortRef,
    isRunningRef,
    activeRunIdRef,
    usageThreadRef,
    setIsRunning,
    setMessages,
    usageLifecycle,
    prepareUsageBaseline,
    invalidateRuntimeReads,
    onArtifact,
    streamErrorText,
  } = deps;

  /** Take the run: drop the history load in flight, claim the abort slot, start the clock. */
  const beginRun = useCallback(
    (runThreadId: string): AbortController => {
      historyRequestRef.current += 1;
      historyAbortRef.current?.abort();
      historyAbortRef.current = null;
      const controller = new AbortController();
      abortRef.current = controller;
      usageThreadRef.current = runThreadId;
      isRunningRef.current = true;
      setIsRunning(true);
      return controller;
    },
    [abortRef, historyAbortRef, historyRequestRef, isRunningRef, setIsRunning, usageThreadRef],
  );

  /** Release it, but only if this controller is still the live one: a stream that lost the
   * slot to a newer run must not clear the newer run's state on its way out. */
  const endRun = useCallback(
    (controller: AbortController, runThreadId: string, usageRunId: number) => {
      usageLifecycle.settle(usageRunId);
      if (abortRef.current === controller) {
        isRunningRef.current = false;
        setIsRunning(false);
        abortRef.current = null;
        activeRunIdRef.current = null;
      }
      invalidateRuntimeReads(runThreadId);
    },
    [abortRef, activeRunIdRef, invalidateRuntimeReads, isRunningRef, setIsRunning, usageLifecycle],
  );

  const foldReRun = useCallback(
    async (url: string, body: unknown, base: ThreadMessageLike[]) => {
      const controller = beginRun(threadId);
      const usageRunId = usageLifecycle.start();
      try {
        if (!(await prepareUsageBaseline(threadId, usageRunId, controller.signal))) return;
        await streamPost({
          url,
          body,
          signal: controller.signal,
          ...(onArtifact !== undefined ? { onArtifact } : {}),
          onUpdate: (assistant, usage) => {
            usageLifecycle.update(usageRunId, usage);
            setMessages([...base, assistant]);
          },
        });
      } catch (error) {
        if (!isAbortError(error)) {
          setMessages([...base, assistantErrorMessage(streamErrorText)]);
        }
      } finally {
        endRun(controller, threadId, usageRunId);
      }
    },
    [
      beginRun,
      endRun,
      onArtifact,
      prepareUsageBaseline,
      setMessages,
      streamErrorText,
      threadId,
      usageLifecycle,
    ],
  );

  const foldAppendedStream = useCallback<StreamFolds['foldAppendedStream']>(
    async (runThreadId, run) => {
      const controller = beginRun(runThreadId);
      let appended = false;
      const usageRunId = usageLifecycle.start();
      try {
        if (!(await prepareUsageBaseline(runThreadId, usageRunId, controller.signal))) return;
        await run(controller, (assistant, usage) => {
          usageLifecycle.update(usageRunId, usage);
          setMessages((prev) => {
            if (!appended) {
              appended = true;
              return [...prev, assistant];
            }
            const next = prev.slice();
            next[next.length - 1] = assistant;
            return next;
          });
        });
      } catch (error) {
        if (!isAbortError(error)) {
          const message = assistantErrorMessage(streamErrorText);
          setMessages((prev) => (appended ? [...prev.slice(0, -1), message] : [...prev, message]));
        }
      } finally {
        endRun(controller, runThreadId, usageRunId);
      }
    },
    [beginRun, endRun, prepareUsageBaseline, setMessages, streamErrorText, usageLifecycle],
  );

  const foldResumeRun = useCallback(
    (resumeThreadId: string) =>
      foldAppendedStream(resumeThreadId, (controller, onUpdate) =>
        streamPost({
          url: '/agent/run',
          body: { threadId: resumeThreadId, messages: [] },
          signal: controller.signal,
          ...(onArtifact !== undefined ? { onArtifact } : {}),
          onUpdate,
        }),
      ),
    [foldAppendedStream, onArtifact],
  );

  return { foldReRun, foldAppendedStream, foldResumeRun };
}
