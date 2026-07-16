import { useCallback, useLayoutEffect, useRef } from 'react';
import type { Approval } from '../approvals/useApprovals';

interface FocusIntent {
  readonly session: number;
  readonly request: number;
}

export function useApprovalFocus(
  threadId: string,
  approvalLocked: boolean,
  isRunning: boolean,
  input: HTMLTextAreaElement | null,
): (next: Approval | undefined) => void {
  const sessionEpoch = useRef(0);
  const requestEpoch = useRef(0);
  const frameId = useRef<number | undefined>(undefined);
  const pendingComposer = useRef<FocusIntent | undefined>(undefined);
  const locked = useRef(approvalLocked);
  const running = useRef(isRunning);
  const currentInput = useRef(input);
  const previouslyLocked = useRef(approvalLocked);

  const cancelFrame = useCallback(() => {
    if (frameId.current === undefined) return;
    cancelAnimationFrame(frameId.current);
    frameId.current = undefined;
  }, []);

  const invalidateRequest = useCallback(() => {
    requestEpoch.current += 1;
    pendingComposer.current = undefined;
    cancelFrame();
  }, [cancelFrame]);

  const schedulePendingComposer = useCallback(() => {
    const intent = pendingComposer.current;
    const inputTarget = currentInput.current;
    if (
      intent === undefined ||
      locked.current ||
      inputTarget === null ||
      inputTarget.disabled ||
      frameId.current !== undefined
    ) {
      return;
    }
    frameId.current = requestAnimationFrame(() => {
      frameId.current = undefined;
      if (
        pendingComposer.current !== intent ||
        sessionEpoch.current !== intent.session ||
        requestEpoch.current !== intent.request ||
        locked.current
      ) {
        return;
      }
      const target = currentInput.current;
      if (target === null || target.disabled) return;
      pendingComposer.current = undefined;
      target.focus();
    });
  }, []);

  useLayoutEffect(() => {
    sessionEpoch.current += 1;
    invalidateRequest();
    return () => {
      sessionEpoch.current += 1;
      invalidateRequest();
    };
  }, [invalidateRequest, threadId]);

  useLayoutEffect(() => {
    const becameLocked = !previouslyLocked.current && approvalLocked;
    locked.current = approvalLocked;
    running.current = isRunning;
    currentInput.current = input;
    previouslyLocked.current = approvalLocked;
    if (becameLocked) {
      invalidateRequest();
      return;
    }
    if (!approvalLocked) schedulePendingComposer();
  }, [approvalLocked, input, invalidateRequest, isRunning, schedulePendingComposer]);

  return useCallback(
    (next: Approval | undefined) => {
      const intent = {
        session: sessionEpoch.current,
        request: requestEpoch.current + 1,
      };
      requestEpoch.current = intent.request;
      pendingComposer.current = undefined;
      cancelFrame();

      if (next === undefined) {
        pendingComposer.current = intent;
        schedulePendingComposer();
        return;
      }

      frameId.current = requestAnimationFrame(() => {
        frameId.current = undefined;
        if (sessionEpoch.current !== intent.session || requestEpoch.current !== intent.request) {
          return;
        }
        const target =
          typeof CSS !== 'undefined' && typeof CSS.escape === 'function'
            ? document.querySelector<HTMLElement>(
                `[data-approval-token="${CSS.escape(next.token)}"]`,
              )
            : Array.from(document.querySelectorAll<HTMLElement>('[data-approval-token]')).find(
                (element) => element.dataset.approvalToken === next.token,
              );
        target?.focus();
      });
    },
    [cancelFrame, schedulePendingComposer],
  );
}
