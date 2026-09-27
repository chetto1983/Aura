import { describe, expect, it } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import {
  applyElicitationSignal,
  keepLiveRun,
  useThreadElicitations,
  type ElicitationItem,
} from '../useThreadElicitations';

function question(id: string, runId = 'run-1'): ElicitationQuestion {
  return {
    run_id: runId,
    id,
    server: 'forms',
    message: 'm',
    fields: [],
    deadline: '2026-09-25T10:05:00Z',
  };
}

const asked = (id: string, runId?: string) => ({
  kind: 'question' as const,
  question: question(id, runId),
});
const resolved = (id: string, action: 'accept' | 'decline' | 'cancel', expired?: true) => ({
  kind: 'resolved' as const,
  resolved: { id, action, ...(expired ? { expired } : {}) },
});

function fold(...signals: Parameters<typeof applyElicitationSignal>[1][]) {
  return signals.reduce<readonly ElicitationItem[]>(applyElicitationSignal, []);
}

describe('applyElicitationSignal', () => {
  it('keeps questions in arrival order', () => {
    expect(fold(asked('a'), asked('b')).map((item) => item.question.id)).toEqual(['a', 'b']);
  });

  // Review Focus 4: a reload replays the run from its first frame, and the run's own list may
  // bring the same form again.
  it('holds a replayed question once, and its resolution still applies', () => {
    const items = fold(asked('a'), resolved('a', 'accept'), asked('a'), resolved('a', 'accept'));
    expect(items).toHaveLength(1);
    expect(items[0]?.outcome).toBe('accepted');
  });

  it('settles a question from its first resolution only', () => {
    expect(fold(asked('a'), resolved('a', 'cancel'), resolved('a', 'accept'))[0]?.outcome).toBe(
      'cancelled',
    );
  });

  it('tells an expiry from a cancel for another reason', () => {
    expect(fold(asked('a'), resolved('a', 'cancel', true))[0]?.outcome).toBe('expired');
    expect(fold(asked('b'), resolved('b', 'cancel'))[0]?.outcome).toBe('cancelled');
    expect(fold(asked('c'), resolved('c', 'decline'))[0]?.outcome).toBe('declined');
  });

  it('ignores a resolution for a question it never saw', () => {
    expect(fold(asked('a'), resolved('zzz', 'accept'))[0]?.outcome).toBeUndefined();
  });

  it('drops every card of an earlier run when a new run asks, settled or not', () => {
    const items = fold(asked('a', 'run-1'), resolved('a', 'accept'), asked('b', 'run-1'));
    expect(applyElicitationSignal(items, asked('c', 'run-2')).map((i) => i.question.id)).toEqual([
      'c',
    ]);
  });
});

describe('keepLiveRun', () => {
  it("keeps only the live run's cards, and the same list when nothing goes", () => {
    const items = fold(asked('a', 'run-1'), asked('b', 'run-1'));
    expect(keepLiveRun(items, 'run-1')).toBe(items);
    expect(keepLiveRun(items, undefined)).toEqual([]);
  });
});

describe('useThreadElicitations', () => {
  it('forgets an old thread and rejects its late callbacks', () => {
    const { result, rerender } = renderHook(
      ({ threadId }) => useThreadElicitations(threadId, true, undefined),
      { initialProps: { threadId: 't-1' } },
    );
    const oldSignal = result.current.onSignal;
    const oldSnapshot = result.current.onSnapshot;
    act(() => {
      oldSignal(asked('old'));
    });
    rerender({ threadId: 't-2' });
    act(() => {
      result.current.onSignal(asked('new', 'run-2'));
    });
    act(() => {
      oldSignal(asked('late'));
      oldSnapshot('run-1', [question('late')]);
    });
    expect(result.current.items.map((item) => item.question.id)).toEqual(['new']);
    rerender({ threadId: 't-1' });
    expect(result.current.items).toEqual([]);
  });

  it('does not let a delayed list revive a retired run', () => {
    const { result, rerender } = renderHook(
      ({ isRunning }) => useThreadElicitations('t-1', isRunning, undefined),
      { initialProps: { isRunning: true } },
    );
    act(() => {
      result.current.onSignal(asked('old'));
    });
    rerender({ isRunning: false });
    rerender({ isRunning: true });
    act(() => {
      result.current.onSignal(asked('new', 'run-2'));
    });
    act(() => {
      result.current.onSignal(asked('late', 'run-1'));
      result.current.onSnapshot('run-1', [question('late')]);
    });
    expect(result.current.items.map((item) => item.question.id)).toEqual(['new']);
  });

  it('does not let an earlier run replace the run that superseded it', () => {
    const { result } = renderHook(() => useThreadElicitations('t-1', true, undefined));
    act(() => {
      result.current.onSignal(asked('old'));
    });
    act(() => {
      result.current.onSignal(asked('new', 'run-2'));
    });
    act(() => {
      result.current.onSignal(asked('late', 'run-1'));
    });
    expect(result.current.items.map((item) => item.question.id)).toEqual(['new']);
  });

  it('scopes its forms to the thread it serves', () => {
    const { result, rerender } = renderHook(
      ({ threadId }) => useThreadElicitations(threadId, true, undefined),
      { initialProps: { threadId: 't-1' } },
    );
    act(() => {
      result.current.onSignal(asked('a'));
    });
    expect(result.current.items).toHaveLength(1);
    rerender({ threadId: 't-2' });
    expect(result.current.items).toHaveLength(0);
  });

  it('reorders a late list while preserving receipts and newer stream-only forms', () => {
    const { result } = renderHook(() => useThreadElicitations('t-1', true, undefined));
    act(() => {
      result.current.onSignal(asked('b'));
      result.current.onSignal(resolved('b', 'accept'));
      result.current.onSignal(asked('c'));
      result.current.onSnapshot('run-1', [
        question('a'),
        question('b'),
        question('wrong', 'run-2'),
      ]);
    });
    expect(result.current.items.map((item) => [item.question.id, item.outcome])).toEqual([
      ['a', undefined],
      ['b', 'accepted'],
      ['c', undefined],
    ]);
  });

  // A Stop aborts the stream before the cancel's resolution can arrive: the form still goes
  // with its run instead of staying live-looking for the rest of the thread.
  it('drops an unsettled form once the thread stops and the server no longer runs it', () => {
    const { result, rerender } = renderHook(
      ({ isRunning, liveRunId }) => useThreadElicitations('t-1', isRunning, liveRunId),
      { initialProps: { isRunning: true, liveRunId: undefined as string | undefined } },
    );
    act(() => {
      result.current.onSignal(asked('a', 'run-1'));
    });
    rerender({ isRunning: false, liveRunId: 'run-1' });
    expect(result.current.items).toHaveLength(1);
    rerender({ isRunning: false, liveRunId: undefined });
    expect(result.current.items).toHaveLength(0);
  });
});
