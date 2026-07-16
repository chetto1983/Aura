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

function row(token: string): Approval {
  return {
    token,
    conversation_id: 'a',
    kind: 'clarification',
    question: token,
    priority: 0,
  };
}

function resolve(
  value: ReturnType<typeof useThreadApprovals>,
  approval: Approval,
  action: ResolveAction,
): Promise<void> {
  const attempt = { approval, action, attemptId: `${approval.token}-${action}` };
  let promise: Promise<void> | undefined;
  act(() => {
    value.onResolutionStarted(attempt);
    promise = value.onResolved(attempt);
  });
  if (promise === undefined) throw new Error('resolution did not start');
  return promise;
}

beforeEach(() => {
  h.data = undefined;
  h.refetch.mockReset();
});

describe('useThreadApprovals Cancel focus recovery', () => {
  it.each(['throw', 'error', 'stale'] as const)(
    'focuses the composer once without resuming after a %s refetch later polls to zero',
    async (resultKind) => {
      const approval = row(`cancel-${resultKind}`);
      h.data = [approval];
      if (resultKind === 'throw') h.refetch.mockRejectedValueOnce(new Error('offline'));
      else if (resultKind === 'error') {
        h.refetch.mockResolvedValueOnce({ data: [], error: new Error('offline') });
      } else h.refetch.mockResolvedValueOnce({ data: [approval] });
      const onResume = vi.fn(() => Promise.resolve());
      const onFocus = vi.fn();
      const { result, rerender } = renderHook(() => useThreadApprovals('a', onResume, onFocus));

      await act(async () => resolve(result.current, approval, 'cancel'));
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
      rerender();
      expect(onFocus).toHaveBeenCalledTimes(1);
      expect(onResume).not.toHaveBeenCalled();
    },
  );
});
