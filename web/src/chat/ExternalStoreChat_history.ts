import { useEffect, useRef, useState, type RefObject } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { assistantErrorMessage, isAbortSignalAborted } from './ExternalStoreChat_folds';
import { importThreadTree, type TreeImporter } from './ExternalStoreChat_tree';
import { fetchThreadHistoryTree } from './threadHistory';

// ExternalStoreChat_history — the thread's history load, split out of ExternalStoreChat.tsx
// on touch (600-LOC cap, the _streams/_branches precedent) when the load came to import the
// conversation tree (spec 2026-10-27). A thread change detaches the run this tab was viewing,
// clears the list, and loads the new thread: every branch goes into the runtime, which hands
// the one on screen back through onImport.

export interface HistoryReadiness {
  readonly threadId: string;
  readonly status: 'loading' | 'ready' | 'error';
}

export interface ThreadHistoryArgs {
  readonly threadId: string;
  /** The runtime's thread, set once the runtime exists (it is created after this hook). */
  readonly runtimeRef: RefObject<{ readonly thread: TreeImporter } | null>;
  readonly usageThreadRef: RefObject<string | null>;
  readonly isRunningRef: RefObject<boolean>;
  readonly abortRef: RefObject<AbortController | null>;
  readonly activeRunIdRef: RefObject<string | null>;
  /** Bumped by every history load and every send; a response from an older one is dropped. */
  readonly historyRequestRef: RefObject<number>;
  readonly historyAbortRef: RefObject<AbortController | null>;
  readonly setIsRunning: (running: boolean) => void;
  readonly setMessages: (messages: ThreadMessageLike[]) => void;
  readonly usageLifecycle: { readonly clear: () => void };
  readonly loadErrorText: string;
}

export function useThreadHistory({
  threadId,
  runtimeRef,
  usageThreadRef,
  isRunningRef,
  abortRef,
  activeRunIdRef,
  historyRequestRef,
  historyAbortRef,
  setIsRunning,
  setMessages,
  usageLifecycle,
  loadErrorText,
}: ThreadHistoryArgs): HistoryReadiness {
  const [historyReadiness, setHistoryReadiness] = useState<HistoryReadiness>({
    threadId,
    status: threadId.length === 0 ? 'ready' : 'loading',
  });
  const historyReadinessRef = useRef(historyReadiness);
  useEffect(() => {
    historyReadinessRef.current = historyReadiness;
  }, [historyReadiness]);

  useEffect(() => {
    const isFirstThreadAdoption = usageThreadRef.current === threadId;
    if (isFirstThreadAdoption) {
      usageThreadRef.current = null;
    } else {
      usageLifecycle.clear();
      if (isRunningRef.current) {
        const previousViewer = abortRef.current;
        abortRef.current = null;
        activeRunIdRef.current = null;
        isRunningRef.current = false;
        setIsRunning(false);
        previousViewer?.abort();
      }
    }
    historyRequestRef.current += 1;
    const request = historyRequestRef.current;
    const nextHistoryStatus = threadId.length === 0 ? 'ready' : 'loading';
    queueMicrotask(() => {
      if (request !== historyRequestRef.current) return;
      const current = historyReadinessRef.current;
      if (current.threadId === threadId && current.status === nextHistoryStatus) return;
      const next = { threadId, status: nextHistoryStatus } as const;
      historyReadinessRef.current = next;
      setHistoryReadiness(next);
    });
    const clearLoadedMessages = () => {
      queueMicrotask(() => {
        if (request !== historyRequestRef.current) return;
        setMessages([]);
      });
    };
    historyAbortRef.current?.abort();
    if (threadId.length === 0) {
      historyAbortRef.current = null;
      clearLoadedMessages();
      return;
    }
    if (isFirstThreadAdoption && isRunningRef.current) {
      historyAbortRef.current = null;
      return;
    }

    const controller = new AbortController();
    historyAbortRef.current = controller;
    clearLoadedMessages();

    void fetchThreadHistoryTree(threadId, controller.signal)
      .then((tree) => {
        if (isAbortSignalAborted(controller.signal) || request !== historyRequestRef.current)
          return;
        const runtime = runtimeRef.current;
        if (runtime !== null) importThreadTree(runtime.thread, tree);
        setHistoryReadiness({ threadId, status: 'ready' });
      })
      .catch((err: unknown) => {
        if (
          isAbortSignalAborted(controller.signal) ||
          request !== historyRequestRef.current ||
          (err instanceof DOMException && err.name === 'AbortError')
        ) {
          return;
        }
        setMessages([assistantErrorMessage(loadErrorText)]);
        setHistoryReadiness({ threadId, status: 'error' });
      })
      .finally(() => {
        if (request === historyRequestRef.current) historyAbortRef.current = null;
      });

    return () => {
      controller.abort();
    };
  }, [
    threadId,
    usageLifecycle,
    loadErrorText,
    runtimeRef,
    usageThreadRef,
    isRunningRef,
    abortRef,
    activeRunIdRef,
    historyRequestRef,
    historyAbortRef,
    setIsRunning,
    setMessages,
  ]);

  return historyReadiness;
}
