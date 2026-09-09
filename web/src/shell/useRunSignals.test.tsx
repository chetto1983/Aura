import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { useRunSignals } from './useRunSignals';

function wrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe('useRunSignals', () => {
  it('invalidates the assets of the thread that is active WHEN THE ARTIFACT LANDS', async () => {
    // The regression this pins, measured live on 2026-09-09: sending the first message
    // from the home creates the conversation TOGETHER with the run, so the pump starts
    // while activeThreadId is still "". The artifact frame then arrived, onArtifact ran
    // against a stale closure holding "", took the early return, and the panel was never
    // refetched — 300s of a real turn with the artifact delivered and the panel still
    // reading "Nessun artefatto". Reopening the conversation showed it, because a fresh
    // mount refetches.
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, 'invalidateQueries');
    const openArtifacts = vi.fn();

    const { result, rerender } = renderHook(
      ({ threadId }: { threadId: string }) => useRunSignals(threadId, openArtifacts),
      { wrapper: wrapper(client), initialProps: { threadId: '' } },
    );

    // The pump captures onArtifact ONCE, when the run starts, and holds that reference for
    // the whole stream. So the callback under test is the one taken BEFORE the conversation
    // id existed — capture it here, exactly as streamRunResilient/attachRun do.
    const captured = result.current.onArtifact;

    // The conversation is created moments later, while the run is already streaming.
    rerender({ threadId: 'conv-1' });

    act(() => {
      captured();
    });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['assets', 'conv-1'] });
    expect(openArtifacts).toHaveBeenCalledTimes(1);
  });

  it('stays inert while there is genuinely no thread', () => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, 'invalidateQueries');
    const openArtifacts = vi.fn();
    const { result } = renderHook(() => useRunSignals('', openArtifacts), { wrapper: wrapper(client) });

    act(() => {
      result.current.onArtifact();
    });

    expect(invalidate).not.toHaveBeenCalled();
    expect(openArtifacts).not.toHaveBeenCalled();
  });

  it('auto-opens once per thread, and re-arms for a new one', () => {
    const client = new QueryClient();
    const openArtifacts = vi.fn();
    const { result, rerender } = renderHook(
      ({ threadId }: { threadId: string }) => useRunSignals(threadId, openArtifacts),
      { wrapper: wrapper(client), initialProps: { threadId: 'conv-1' } },
    );

    act(() => {
      result.current.onArtifact();
      result.current.onArtifact();
    });
    expect(openArtifacts).toHaveBeenCalledTimes(1);

    rerender({ threadId: 'conv-2' });
    act(() => {
      result.current.onArtifact();
    });
    expect(openArtifacts).toHaveBeenCalledTimes(2);
  });
});
