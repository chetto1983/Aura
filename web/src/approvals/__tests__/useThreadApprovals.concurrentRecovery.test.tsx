import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Approval, ResolveAction } from '../useApprovals';
import { useThreadApprovals } from '../useThreadApprovals';

const h = vi.hoisted(() => ({
  data: undefined as Approval[] | undefined,
  refetch: vi.fn(),
}));

vi.mock('../useApprovals', async () => {
  const actual = await vi.importActual<typeof import('../useApprovals')>('../useApprovals');
  return {
    ...actual,
    useApprovals: () => ({ data: h.data, refetch: h.refetch }),
  };
});

interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly resolve: (value: T) => void;
  readonly reject: (reason?: unknown) => void;
}

function deferred<T>(): Deferred<T> {
  let resolvePromise: ((value: T) => void) | undefined;
  let rejectPromise: ((reason?: unknown) => void) | undefined;
  const promise = new Promise<T>((resolve, reject) => {
    resolvePromise = resolve;
    rejectPromise = reject;
  });
  return {
    promise,
    resolve(value) {
      if (resolvePromise === undefined) throw new Error('resolver not initialized');
      resolvePromise(value);
    },
    reject(reason) {
      if (rejectPromise === undefined) throw new Error('rejecter not initialized');
      rejectPromise(reason);
    },
  };
}

function row(token: string): Approval {
  return {
    token,
    conversation_id: 'a',
    kind: 'clarification',
    question: token,
    priority: 0,
  };
}

let attemptSequence = 0;

function resolve(
  value: ReturnType<typeof useThreadApprovals>,
  approval: Approval,
  action: ResolveAction,
): Promise<void> {
  const attempt = {
    approval,
    action,
    attemptId: `${approval.token}-${action}-${String(++attemptSequence)}`,
  };
  let promise: Promise<void> | undefined;
  act(() => {
    value.onResolutionStarted(attempt);
    promise = value.onResolved(attempt);
  });
  if (promise === undefined) throw new Error('resolution did not start');
  return promise;
}

async function settleUncertain(
  pending: Deferred<{ data: Approval[]; error?: Error }>,
  resultKind: 'throw' | 'error' | 'stale',
  staleRows: Approval[],
): Promise<void> {
  if (resultKind === 'throw') pending.reject(new Error('offline'));
  else if (resultKind === 'error') pending.resolve({ data: [], error: new Error('offline') });
  else pending.resolve({ data: staleRows });
  await Promise.resolve();
}

beforeEach(() => {
  h.data = undefined;
  h.refetch.mockReset();
  attemptSequence = 0;
});

describe('useThreadApprovals concurrent recovery ownership', () => {
  it.each([
    ['accept', 'decline', 'throw', 'a-first'],
    ['decline', 'accept', 'error', 'a-first'],
    ['accept', 'accept', 'stale', 'a-first'],
    ['decline', 'decline', 'throw', 'b-first'],
    ['accept', 'decline', 'error', 'b-first'],
    ['decline', 'accept', 'stale', 'b-first'],
  ] as const)(
    'preserves concurrent A %s and B %s recovery when A confirms B (%s, %s)',
    async (actionA, actionB, resultKind, completionOrder) => {
      const approvalA = row(`concurrent-a-${resultKind}-${completionOrder}`);
      const approvalB = row(`concurrent-b-${resultKind}-${completionOrder}`);
      h.data = [approvalA, approvalB];
      const refreshA = deferred<{ data: Approval[]; error?: Error }>();
      const refreshB = deferred<{ data: Approval[]; error?: Error }>();
      h.refetch.mockReturnValueOnce(refreshA.promise).mockReturnValueOnce(refreshB.promise);
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      const answerA = resolve(result.current, approvalA, actionA);
      const answerB = resolve(result.current, approvalB, actionB);
      await act(async () => {
        if (completionOrder === 'a-first') {
          refreshA.resolve({ data: [approvalB] });
          await answerA;
          await settleUncertain(refreshB, resultKind, [approvalB]);
          await answerB;
        } else {
          await settleUncertain(refreshB, resultKind, [approvalB]);
          await answerB;
          refreshA.resolve({ data: [approvalB] });
          await answerA;
        }
      });

      expect(onFocus).toHaveBeenCalledTimes(1);
      expect(onFocus).toHaveBeenCalledWith(approvalB);
      expect(onResume).not.toHaveBeenCalled();

      await act(async () => {
        h.data = [];
        rerender();
        await Promise.resolve();
      });

      expect(onFocus).toHaveBeenCalledTimes(2);
      expect(onFocus).toHaveBeenLastCalledWith(undefined);
      expect(onResume).toHaveBeenCalledTimes(1);
      rerender();
      expect(onResume).toHaveBeenCalledTimes(1);
    },
  );

  it.each([
    ['accept', 'throw'],
    ['decline', 'error'],
    ['accept', 'stale'],
  ] as const)(
    'focuses B once when A %s has %s recovery and a poll confirms B remains',
    async (action, resultKind) => {
      const approvalA = row(`handoff-a-${resultKind}`);
      const approvalB = row(`handoff-b-${resultKind}`);
      h.data = [approvalA, approvalB];
      if (resultKind === 'throw') h.refetch.mockRejectedValueOnce(new Error('offline'));
      else if (resultKind === 'error') {
        h.refetch.mockResolvedValueOnce({
          data: [approvalA, approvalB],
          error: new Error('offline'),
        });
      } else h.refetch.mockResolvedValueOnce({ data: [approvalA, approvalB] });
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      await act(async () => resolve(result.current, approvalA, action));
      h.data = [approvalB];
      rerender();

      expect(onFocus).toHaveBeenCalledTimes(1);
      expect(onFocus).toHaveBeenCalledWith(approvalB);
      expect(onResume).not.toHaveBeenCalled();
      rerender();
      expect(onFocus).toHaveBeenCalledTimes(1);
    },
  );

  it.each(['throw', 'error', 'stale'] as const)(
    'keeps Cancel B focus recovery when earlier Accept A completes after Cancel %s',
    async (resultKind) => {
      const approvalA = row(`mixed-a-${resultKind}`);
      const approvalB = row(`mixed-b-${resultKind}`);
      h.data = [approvalA, approvalB];
      const refreshA = deferred<{ data: Approval[]; error?: Error }>();
      const refreshB = deferred<{ data: Approval[]; error?: Error }>();
      h.refetch.mockReturnValueOnce(refreshA.promise).mockReturnValueOnce(refreshB.promise);
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      const answerA = resolve(result.current, approvalA, 'accept');
      const cancelB = resolve(result.current, approvalB, 'cancel');
      await act(async () => {
        await settleUncertain(refreshB, resultKind, [approvalB]);
        await cancelB;
        refreshA.resolve({ data: [approvalB] });
        await answerA;
      });
      expect(onFocus).not.toHaveBeenCalled();
      expect(onResume).not.toHaveBeenCalled();

      await act(async () => {
        h.data = [];
        rerender();
        await Promise.resolve();
      });

      expect(onFocus).toHaveBeenCalledTimes(1);
      expect(onFocus).toHaveBeenCalledWith(undefined);
      expect(onResume).not.toHaveBeenCalled();
      rerender();
      expect(onFocus).toHaveBeenCalledTimes(1);
    },
  );
});
