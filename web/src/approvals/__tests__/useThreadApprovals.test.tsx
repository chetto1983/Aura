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
}

function deferred<T>(): Deferred<T> {
  let resolvePromise: ((value: T) => void) | undefined;
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve;
  });
  return {
    promise,
    resolve(value) {
      if (resolvePromise === undefined) throw new Error('deferred resolver was not initialized');
      resolvePromise(value);
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
  onResolved: (resolution: { approval: Approval; action: ResolveAction }) => Promise<void>,
  approval: Approval,
  action: ResolveAction,
): Promise<void> {
  let promise: Promise<void> | undefined;
  act(() => {
    promise = onResolved({ approval, action });
  });
  if (promise === undefined) throw new Error('resolution did not start');
  return promise;
}

beforeEach(() => {
  h.data = undefined;
  h.refetch.mockReset();
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
    await act(async () => {
      await result.current.onResolved({ approval: pending, action: 'accept' });
    });

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

    await act(async () => {
      await result.current.onResolved({ approval: resolved, action: 'decline' });
    });

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

    const inFlight = startResolution(result.current.onResolved, approvalA, 'accept');
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

      const cancel = startResolution(result.current.onResolved, cancelApproval, 'cancel');
      const answer = startResolution(result.current.onResolved, answerApproval, answerAction);
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

    const first = startResolution(result.current.onResolved, approval, 'cancel');
    const second = startResolution(result.current.onResolved, approval, 'cancel');
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
