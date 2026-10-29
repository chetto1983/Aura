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

/** The branch the transcript draws. */
export interface BranchOnScreen {
  /** Its last turn; undefined while the last message has no seq, or nothing is drawn. */
  readonly leafSeq: number | undefined;
  /** Whether a drawn message starts or ends at the seq. */
  readonly shows: (seq: number) => boolean;
  /** False while the transcript is empty: there is nothing to put a marker on. */
  readonly drawn: boolean;
}

/**
 * The compaction of the branch on screen, which `branch.leafSeq` ends (undefined: the turn
 * written last). The read is keyed by the leaf, so it is made again when a run moves the leaf
 * or a switch changes the branch (spec 2026-10-27, D14).
 *
 * Until that read answers, the previous marker stays if the branch still shows its leaf: the
 * branch only grew, so the marker still describes turns on screen, and dropping it would shift
 * the transcript on every run. After a switch the previous leaf is gone, and so is its marker.
 */
export function useCompaction(threadId: string, branch: BranchOnScreen): CompactionHandle {
  const { leafSeq, shows, drawn } = branch;
  const queryClient = useQueryClient();
  const [failure, setFailure] = useState<CompactionFailure | undefined>(undefined);
  const { data } = useQuery({
    queryKey: [COMPACTION_KEY, threadId, leafSeq ?? null],
    queryFn: ({ signal }) => fetchCompaction(threadId, leafSeq, signal),
    enabled: threadId.length > 0 && drawn,
    retry: false,
    placeholderData: (previous, previousQuery) => {
      const [, previousThread, previousLeaf] = previousQuery?.queryKey ?? [];
      // A read with no leaf was made while the last message had no seq yet: a run of this
      // tab, on the branch on screen.
      const sameBranch =
        previousLeaf === null || (typeof previousLeaf === 'number' && shows(previousLeaf));
      return previousThread === threadId && sameBranch ? previous : undefined;
    },
  });

  const mutation = useMutation({
    mutationFn: (leaf: number | undefined) => compactConversation(threadId, leaf),
    onSuccess: (result, leaf) => {
      // The POST already carries the new state, so the marker moves on the response rather
      // than on a follow-up read: the summarizer call took seconds, and a second round-trip
      // to learn what we were just told would be visible.
      queryClient.setQueryData([COMPACTION_KEY, threadId, leaf ?? null], result);
    },
    onError: (error: unknown) => {
      setFailure(error instanceof CompactionError ? error.reason : 'failed');
    },
  });

  const { mutate } = mutation;
  const run = useCallback(() => {
    if (threadId.length === 0) return;
    setFailure(undefined);
    mutate(leafSeq);
  }, [mutate, threadId, leafSeq]);

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
