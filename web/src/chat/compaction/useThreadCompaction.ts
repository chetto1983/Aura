import { useCallback, useMemo } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { COMPACT_COMMAND } from '../composer/commands';
import { compactionAnchorId } from '../messageSeq';
import { useCompaction } from './useCompaction';
import type { CompactionFailure, CompactionState } from './api';

// useThreadCompaction — everything the chat lane needs to know about compaction, in one
// value: the command executor the composer runs, the state the marker renders, and WHERE in
// the visible transcript that marker belongs.
//
// It exists so the chat lane holds the compaction as a single concern rather than three
// pieces wired separately at three points in a 600-line component — the shape the '/' menu's
// second command would otherwise have to be wired into all over again.

export interface ThreadCompaction {
  readonly state: CompactionState;
  readonly running: boolean;
  readonly failure: CompactionFailure | undefined;
  readonly dismissFailure: () => void;
  /** The id of the message the marker sits under; undefined ⇒ no marker. */
  readonly anchorId: string | undefined;
  /** Runs a '/' command by name. Unknown names are ignored rather than guessed at. */
  readonly runCommand: (name: string) => void;
}

export function useThreadCompaction(
  threadId: string,
  messages: readonly ThreadMessageLike[],
): ThreadCompaction {
  const compaction = useCompaction(threadId);
  const { run, state } = compaction;

  const runCommand = useCallback(
    (name: string) => {
      if (name === COMPACT_COMMAND) run();
    },
    [run],
  );

  const anchorId = useMemo(
    () => compactionAnchorId(messages, state.covers_through_seq),
    [messages, state.covers_through_seq],
  );

  return {
    state,
    running: compaction.running,
    failure: compaction.failure,
    dismissFailure: compaction.dismissFailure,
    anchorId,
    runCommand,
  };
}
