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

interface ResolutionAttempt {
  readonly approval: Approval;
  readonly action: ResolveAction;
  readonly attemptId: string;
}

interface ApprovalLifecycle {
  readonly onResolutionStarted?: (attempt: ResolutionAttempt) => void;
  readonly onResolutionFailed?: (attempt: ResolutionAttempt) => void | Promise<void>;
  readonly onResolved: (attempt: ResolutionAttempt) => Promise<void>;
}

function lifecycle(value: ReturnType<typeof useThreadApprovals>): ApprovalLifecycle {
  return value;
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
      if (resolvePromise === undefined) throw new Error('deferred resolver was not initialized');
      resolvePromise(value);
    },
    reject(reason) {
      if (rejectPromise === undefined) throw new Error('deferred rejecter was not initialized');
      rejectPromise(reason);
    },
  };
}

function row(token: string, conversationId: string, over: Partial<Approval> = {}): Approval {
  return {
    token,
    conversation_id: conversationId,
    kind: 'clarification',
    question: token,
    priority: 0,
    ...over,
  };
}

function startResolution(
  value: ReturnType<typeof useThreadApprovals>,
  approval: Approval,
  action: ResolveAction,
  attemptId = `${approval.token}-${action}-${String(++attemptSequence)}`,
): Promise<void> {
  const attempt = { approval, action, attemptId };
  let promise: Promise<void> | undefined;
  act(() => {
    const current = lifecycle(value);
    current.onResolutionStarted?.(attempt);
    promise = current.onResolved(attempt);
  });
  if (promise === undefined) throw new Error('resolution did not start');
  return promise;
}

let attemptSequence = 0;

beforeEach(() => {
  h.data = undefined;
  h.refetch.mockReset();
  attemptSequence = 0;
});

describe('useThreadApprovals truthful pending gate', () => {
  it('renders terminal rows but does not count a terminal-only thread as pending', () => {
    const expired = row('expired', 'a', { terminal: true });
    h.data = [expired];
    const { result } = renderHook(() =>
      useThreadApprovals(
        'a',
        vi.fn(() => Promise.resolve()),
        vi.fn(),
      ),
    );

    expect(result.current.approvals).toEqual([expired]);
    expect(result.current.isPending).toBe(false);
  });

  it('preserves mixed backend order, locks only for unresolved rows, and resumes past terminal rows', async () => {
    const expiredFirst = row('expired-first', 'a', { terminal: true });
    const pending = row('pending', 'a');
    const expiredLast = row('expired-last', 'a', { terminal: true });
    h.data = [expiredFirst, pending, expiredLast];
    h.refetch.mockResolvedValueOnce({ data: [expiredFirst, expiredLast] });
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

    expect(result.current.approvals).toEqual([expiredFirst, pending, expiredLast]);
    expect(result.current.isPending).toBe(true);
    await act(async () => startResolution(result.current, pending, 'accept'));

    expect(onFocus).toHaveBeenCalledTimes(1);
    expect(onFocus).toHaveBeenCalledWith(undefined);
    expect(onResume).toHaveBeenCalledTimes(1);
  });

  it('focuses the next unresolved row while terminal rows remain in display order', async () => {
    const expiredFirst = row('expired-first', 'a', { terminal: true });
    const resolved = row('resolved', 'a');
    const expiredMiddle = row('expired-middle', 'a', { terminal: true });
    const next = row('next', 'a');
    h.data = [expiredFirst, resolved, expiredMiddle, next];
    h.refetch.mockResolvedValueOnce({ data: [expiredFirst, expiredMiddle, next] });
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

    await act(async () => startResolution(result.current, resolved, 'decline'));

    expect(onFocus).toHaveBeenCalledWith(next);
    expect(onResume).not.toHaveBeenCalled();
  });
});

describe('useThreadApprovals async session safety', () => {
  it('drops an in-flight A resolution after A to B to A without touching focus or resume', async () => {
    const approvalA = row('a-1', 'a');
    const approvalB = row('b-1', 'b');
    h.data = [approvalA, approvalB];
    const refresh = deferred<{ data: Approval[] }>();
    h.refetch.mockReturnValueOnce(refresh.promise);
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result, rerender } = renderHook(
      ({ threadId }: { threadId: string }) => useThreadApprovals(threadId, onResume, onFocus),
      { initialProps: { threadId: 'a' } },
    );

    const inFlight = startResolution(result.current, approvalA, 'accept');
    rerender({ threadId: 'b' });
    rerender({ threadId: 'a' });
    await act(async () => {
      refresh.resolve({ data: [] });
      await inFlight;
    });

    expect(onFocus).not.toHaveBeenCalled();
    expect(onResume).not.toHaveBeenCalled();
  });
});

describe('useThreadApprovals pending generation ownership', () => {
  it.each([
    ['accept', 'accept'],
    ['decline', 'decline'],
    ['cancel', 'accept'],
  ] as const)(
    'ignores delayed %s completion after token A is replaced by token B, then permits B %s',
    async (oldAction, newAction) => {
      const approvalA = row('generation-a', 'a');
      const approvalB = row('generation-b', 'a');
      h.data = [approvalA];
      const oldRefresh = deferred<{ data: Approval[] }>();
      h.refetch.mockReturnValueOnce(oldRefresh.promise).mockResolvedValueOnce({ data: [] });
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      const oldResolution = startResolution(result.current, approvalA, oldAction, 'old-a');
      h.data = [approvalB];
      rerender();
      await act(async () => {
        oldRefresh.resolve({ data: [approvalB] });
        await oldResolution;
      });

      expect(onFocus).not.toHaveBeenCalled();
      expect(onResume).not.toHaveBeenCalled();

      await act(async () => startResolution(result.current, approvalB, newAction, 'new-b'));

      expect(onFocus).toHaveBeenCalledTimes(1);
      expect(onFocus).toHaveBeenCalledWith(undefined);
      expect(onResume).toHaveBeenCalledTimes(1);
    },
  );

  it('keeps A,B to B in one generation and focuses the overlapping next row', async () => {
    const approvalA = row('overlap-a', 'a');
    const approvalB = row('overlap-b', 'a');
    h.data = [approvalA, approvalB];
    const refresh = deferred<{ data: Approval[] }>();
    h.refetch.mockReturnValueOnce(refresh.promise);
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

    const answer = startResolution(result.current, approvalA, 'accept');
    h.data = [approvalB];
    rerender();
    await act(async () => {
      refresh.resolve({ data: [approvalB] });
      await answer;
    });

    expect(onFocus).toHaveBeenCalledTimes(1);
    expect(onFocus).toHaveBeenCalledWith(approvalB);
    expect(onResume).not.toHaveBeenCalled();
  });
});

describe('useThreadApprovals durable local outcomes', () => {
  it.each(['accept', 'decline'] as const)(
    'recovers a successful local %s after refetch throws when polling later reaches zero',
    async (action) => {
      const approval = row(`throw-${action}`, 'a');
      h.data = [approval];
      h.refetch.mockRejectedValueOnce(new Error('offline'));
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      await act(async () => startResolution(result.current, approval, action));
      expect(onFocus).not.toHaveBeenCalled();
      expect(onResume).not.toHaveBeenCalled();

      await act(async () => {
        h.data = [];
        rerender();
        await Promise.resolve();
      });

      expect(onFocus).toHaveBeenCalledTimes(1);
      expect(onFocus).toHaveBeenCalledWith(undefined);
      expect(onResume).toHaveBeenCalledTimes(1);
      rerender();
      expect(onResume).toHaveBeenCalledTimes(1);
    },
  );

  it.each([
    ['accept', 'an error result', 'error'],
    ['decline', 'an error result', 'error'],
    ['accept', 'a stale nonempty result', 'stale'],
    ['decline', 'a stale nonempty result', 'stale'],
  ] as const)(
    'retains resume eligibility for %s after refetch returns %s',
    async (action, _label, resultKind) => {
      const approval = row(`${resultKind}-${action}`, 'a');
      h.data = [approval];
      h.refetch.mockResolvedValueOnce(
        resultKind === 'error' ? { data: [], error: new Error('offline') } : { data: [approval] },
      );
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      await act(async () => startResolution(result.current, approval, action));
      expect(onFocus).not.toHaveBeenCalled();
      expect(onResume).not.toHaveBeenCalled();

      await act(async () => {
        h.data = [];
        rerender();
        await Promise.resolve();
      });

      expect(onFocus).toHaveBeenCalledTimes(1);
      expect(onFocus).toHaveBeenCalledWith(undefined);
      expect(onResume).toHaveBeenCalledTimes(1);
    },
  );

  it('keeps an intermediate successful outcome eligible until the overlapping queue reaches zero', async () => {
    const approvalA = row('remaining-a', 'a');
    const approvalB = row('remaining-b', 'a');
    h.data = [approvalA, approvalB];
    h.refetch.mockResolvedValueOnce({ data: [approvalB] });
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

    await act(async () => startResolution(result.current, approvalA, 'decline'));
    expect(onFocus).toHaveBeenCalledWith(approvalB);
    expect(onResume).not.toHaveBeenCalled();

    await act(async () => {
      h.data = [];
      rerender();
      await Promise.resolve();
    });

    expect(onFocus).toHaveBeenLastCalledWith(undefined);
    expect(onResume).toHaveBeenCalledTimes(1);
  });

  it('does not resume while a stale queue never reaches zero', async () => {
    const approval = row('still-pending', 'a');
    h.data = [approval];
    h.refetch.mockResolvedValueOnce({ data: [approval] });
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

    await act(async () => startResolution(result.current, approval, 'accept'));
    rerender();
    rerender();

    expect(onFocus).not.toHaveBeenCalled();
    expect(onResume).not.toHaveBeenCalled();
  });
});

describe('useThreadApprovals cancellation precedence', () => {
  it.each([
    ['accept', 'cancel-first'],
    ['accept', 'answer-first'],
    ['decline', 'cancel-first'],
    ['decline', 'answer-first'],
  ] as const)(
    'suppresses concurrent %s resume when %s completes',
    async (answerAction, completionOrder) => {
      const cancelApproval = row('cancel', 'a');
      const answerApproval = row('answer', 'a');
      h.data = [cancelApproval, answerApproval];
      const cancelRefresh = deferred<{ data: Approval[] }>();
      const answerRefresh = deferred<{ data: Approval[] }>();
      h.refetch
        .mockReturnValueOnce(cancelRefresh.promise)
        .mockReturnValueOnce(answerRefresh.promise);
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      const cancel = startResolution(result.current, cancelApproval, 'cancel');
      const answer = startResolution(result.current, answerApproval, answerAction);
      await act(async () => {
        if (completionOrder === 'cancel-first') {
          cancelRefresh.resolve({ data: [] });
          await cancel;
          answerRefresh.resolve({ data: [] });
          await answer;
        } else {
          answerRefresh.resolve({ data: [] });
          await answer;
          cancelRefresh.resolve({ data: [] });
          await cancel;
        }
      });

      expect(onResume).not.toHaveBeenCalled();
    },
  );

  it('deduplicates concurrent Cancel completion and never resumes', async () => {
    const approval = row('cancel', 'a');
    h.data = [approval];
    const firstRefresh = deferred<{ data: Approval[] }>();
    const secondRefresh = deferred<{ data: Approval[] }>();
    h.refetch.mockReturnValueOnce(firstRefresh.promise).mockReturnValueOnce(secondRefresh.promise);
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

    const first = startResolution(result.current, approval, 'cancel');
    const second = startResolution(result.current, approval, 'cancel');
    await act(async () => {
      secondRefresh.resolve({ data: [] });
      await second;
      firstRefresh.resolve({ data: [] });
      await first;
    });

    expect(onResume).not.toHaveBeenCalled();
    expect(onFocus).toHaveBeenCalledTimes(1);
    expect(onFocus).toHaveBeenCalledWith(undefined);
  });
});

describe('useThreadApprovals pre-mutation cancellation ownership', () => {
  it.each(['accept', 'decline'] as const)(
    'suppresses an in-flight %s completion as soon as Cancel intent starts',
    async (answerAction) => {
      const answerApproval = row('answer', 'a');
      const cancelApproval = row('cancel', 'a');
      h.data = [answerApproval, cancelApproval];
      const answerRefresh = deferred<{ data: Approval[] }>();
      h.refetch.mockReturnValueOnce(answerRefresh.promise);
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result } = renderHook(() => useThreadApprovals('a', onResume, onFocus));
      const cancelAttempt: ResolutionAttempt = {
        approval: cancelApproval,
        action: 'cancel',
        attemptId: 'cancel-1',
      };

      const answer = startResolution(result.current, answerApproval, answerAction);
      act(() => {
        lifecycle(result.current).onResolutionStarted?.(cancelAttempt);
      });
      await act(async () => {
        answerRefresh.resolve({ data: [] });
        await answer;
      });

      expect(onResume).not.toHaveBeenCalled();
      expect(onFocus).not.toHaveBeenCalled();
    },
  );

  it('releases a deferred final answer exactly once when the owning Cancel mutation fails', async () => {
    const answerApproval = row('answer', 'a');
    const cancelApproval = row('cancel', 'a');
    h.data = [answerApproval, cancelApproval];
    const answerRefresh = deferred<{ data: Approval[] }>();
    h.refetch.mockReturnValueOnce(answerRefresh.promise);
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result } = renderHook(() => useThreadApprovals('a', onResume, onFocus));
    const cancelAttempt: ResolutionAttempt = {
      approval: cancelApproval,
      action: 'cancel',
      attemptId: 'cancel-failed',
    };

    const answer = startResolution(result.current, answerApproval, 'accept');
    act(() => {
      lifecycle(result.current).onResolutionStarted?.(cancelAttempt);
    });
    await act(async () => {
      answerRefresh.resolve({ data: [] });
      await answer;
    });
    expect(onResume).not.toHaveBeenCalled();

    await act(async () => {
      await lifecycle(result.current).onResolutionFailed?.(cancelAttempt);
    });

    expect(onFocus).toHaveBeenCalledTimes(1);
    expect(onFocus).toHaveBeenCalledWith(undefined);
    expect(onResume).toHaveBeenCalledTimes(1);
  });

  it('deduplicates repeated Cancel intent and failure signals', async () => {
    const answerApproval = row('answer', 'a');
    const cancelApproval = row('cancel', 'a');
    h.data = [answerApproval, cancelApproval];
    const answerRefresh = deferred<{ data: Approval[] }>();
    h.refetch.mockReturnValueOnce(answerRefresh.promise);
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result } = renderHook(() => useThreadApprovals('a', onResume, onFocus));
    const cancelAttempt: ResolutionAttempt = {
      approval: cancelApproval,
      action: 'cancel',
      attemptId: 'same-attempt',
    };

    const answer = startResolution(result.current, answerApproval, 'decline');
    act(() => {
      lifecycle(result.current).onResolutionStarted?.(cancelAttempt);
      lifecycle(result.current).onResolutionStarted?.(cancelAttempt);
    });
    await act(async () => {
      answerRefresh.resolve({ data: [] });
      await answer;
    });
    expect(onResume).not.toHaveBeenCalled();

    await act(async () => {
      await lifecycle(result.current).onResolutionFailed?.(cancelAttempt);
      await lifecycle(result.current).onResolutionFailed?.(cancelAttempt);
    });

    expect(onFocus).toHaveBeenCalledTimes(1);
    expect(onResume).toHaveBeenCalledTimes(1);
  });

  it('ignores a Cancel intent callback retained from an earlier A session', async () => {
    const answerApproval = row('answer', 'a');
    const cancelApproval = row('cancel', 'a');
    h.data = [answerApproval, cancelApproval, row('b-1', 'b')];
    h.refetch.mockResolvedValueOnce({ data: [] });
    const onResume = vi.fn(() => Promise.resolve());
    const onFocus = vi.fn();
    const { result, rerender } = renderHook(
      ({ threadId }: { threadId: string }) => useThreadApprovals(threadId, onResume, onFocus),
      { initialProps: { threadId: 'a' } },
    );
    const staleLifecycle = lifecycle(result.current);

    rerender({ threadId: 'b' });
    rerender({ threadId: 'a' });
    act(() => {
      staleLifecycle.onResolutionStarted?.({
        approval: cancelApproval,
        action: 'cancel',
        attemptId: 'stale-cancel',
      });
    });
    await act(async () => startResolution(result.current, answerApproval, 'accept'));

    expect(onResume).toHaveBeenCalledTimes(1);
  });
});
