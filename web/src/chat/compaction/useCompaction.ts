import { useCallback, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  CompactionError,
  NO_COMPACTION,
  compactConversation,
  fetchCompaction,
  type CompactionFailure,
  type CompactionState,
} from './api';

// useCompaction — the `/compact` command's state, in the one place both surfaces read it:
// the composer (which runs it and reports the outcome) and the transcript (which draws the
// marker at the watermark). One hook rather than two, so the marker cannot lag the command
// that moved it.

export const COMPACTION_KEY = 'conversation-compaction';

export interface CompactionHandle {
  /** The stored summary, or NO_COMPACTION while loading / when there is none. */
  readonly state: CompactionState;
  /** True while a requested compaction is in flight — the summarizer takes seconds. */
  readonly running: boolean;
  /** Why the last request did not happen; cleared when the next one starts. */
  readonly failure: CompactionFailure | undefined;
  readonly run: () => void;
  readonly dismissFailure: () => void;
}

export function useCompaction(threadId: string): CompactionHandle {
  const queryClient = useQueryClient();
  const [failure, setFailure] = useState<CompactionFailure | undefined>(undefined);
  const { data } = useQuery({
    queryKey: [COMPACTION_KEY, threadId],
    queryFn: ({ signal }) => fetchCompaction(threadId, signal),
    enabled: threadId.length > 0,
    retry: false,
  });

  const mutation = useMutation({
    mutationFn: () => compactConversation(threadId),
    onSuccess: (result) => {
      // The POST already carries the new state, so the marker moves on the response rather
      // than on a follow-up read: the summarizer call took seconds, and a second round-trip
      // to learn what we were just told would be visible.
      queryClient.setQueryData([COMPACTION_KEY, threadId], result);
    },
    onError: (error: unknown) => {
      setFailure(error instanceof CompactionError ? error.reason : 'failed');
    },
  });

  const { mutate } = mutation;
  const run = useCallback(() => {
    if (threadId.length === 0) return;
    setFailure(undefined);
    mutate();
  }, [mutate, threadId]);

  const dismissFailure = useCallback(() => {
    setFailure(undefined);
  }, []);

  return {
    state: data ?? NO_COMPACTION,
    running: mutation.isPending,
    failure,
    run,
    dismissFailure,
  };
}
